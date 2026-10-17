package errorlog

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

// 只接收已遮敏的領域錯誤；不保存 HTTP body、URL 或任意底層錯誤。
type Log struct {
	mu       sync.Mutex
	store    *storage.Store
	records  []domain.ErrorRecord
	readOnly bool
}

func New(s *storage.Store) *Log {
	l := &Log{store: s, records: []domain.ErrorRecord{}}
	if err := s.Read("errors.json", &l.records); err != nil && !errors.Is(err, storage.ErrMissing) {
		l.readOnly = true
		l.records = []domain.ErrorRecord{{ID: domain.ID(), OccurredAt: time.Now().UTC(), Source: "storage", Code: "SCHEMA_UNSUPPORTED", Message: "錯誤紀錄未知或損毀；原檔保留，暫不覆寫", Count: 1}}
	}
	return l
}
func (l *Log) List() []domain.ErrorRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]domain.ErrorRecord{}, l.records...)
}
func (l *Log) Add(e *domain.Error, op, job string) domain.ErrorRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now().UTC()
	r := domain.ErrorRecord{ID: domain.ID(), OccurredAt: now, Source: e.Source, Operation: e.Operation, Code: e.Code, Message: e.Message, Detail: e.Detail, HTTPStatus: e.HTTPStatus, VendorCode: e.VendorCode, RequestID: e.RequestID, JobID: job, Count: 1}
	if r.Source == "" {
		r.Source = "application"
	}
	if r.Operation == "" {
		r.Operation = op
	}
	for index, old := range l.records {
		if now.Sub(old.OccurredAt) < 30*time.Second && old.Code == r.Code && old.Operation == r.Operation && old.HTTPStatus == r.HTTPStatus && old.Message == r.Message && old.JobID == job {
			r.ID = old.ID
			r.Count = old.Count + 1
			l.records = append(l.records[:index:index], l.records[index+1:]...)
			break
		}
	}
	l.records = append([]domain.ErrorRecord{r}, l.records...)
	if len(l.records) > 200 {
		l.records = l.records[:200]
	}
	if l.readOnly || l.store.Write("errors.json", l.records) != nil {
		r.Detail = strings.TrimSpace(r.Detail + " 錯誤紀錄暫時無法持久保存，請保留資料並檢查權限。")
		l.records[0] = r
	}
	return r
}
func (l *Log) Delete(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.readOnly {
		return domain.Fail("SCHEMA_UNSUPPORTED", "原錯誤紀錄無法安全清除")
	}
	next := []domain.ErrorRecord{}
	found := id == ""
	for _, r := range l.records {
		if id == "" || r.ID == id {
			found = true
			continue
		}
		next = append(next, r)
	}
	if !found {
		return domain.Fail("NOT_FOUND", "錯誤紀錄已不存在")
	}
	if err := l.store.Write("errors.json", next); err != nil {
		return domain.Fail("STORAGE_ERROR", "錯誤紀錄清除未完成")
	}
	l.records = next
	return nil
}
