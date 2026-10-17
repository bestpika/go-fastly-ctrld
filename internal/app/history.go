package app

import (
	"errors"
	"sort"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

const jobHistoryFile = "history-hidden.json"

// 列表刪除獨立保存，不修改復原、規則所有權與自動冷卻使用的 journal。
type jobHistoryIndex struct {
	IDs []string `json:"ids"`
}

func (c *Core) loadJobHistory() error {
	var index jobHistoryIndex
	err := c.data.Read(jobHistoryFile, &index)
	if errors.Is(err, storage.ErrMissing) {
		c.hiddenJobs = map[string]bool{}
		return nil
	}
	if err != nil || index.IDs == nil {
		return domain.Fail("SCHEMA_UNSUPPORTED", "執行紀錄索引損毀，請檢查原檔；安全底帳保留")
	}
	c.hiddenJobs = map[string]bool{}
	for _, id := range index.IDs {
		if id == "" || c.hiddenJobs[id] {
			return domain.Fail("SCHEMA_UNSUPPORTED", "執行紀錄索引無效，請檢查原檔；安全底帳保留")
		}
		c.hiddenJobs[id] = true
	}
	return nil
}

// 呼叫端須持有 gate；終態仍可能有未決寫入或尚未釋放的執行槽。
func (c *Core) canDeleteJobHistory(j domain.Job) bool {
	if j.ID == c.testID || j.ID == c.writerID || j.Pending() {
		return false
	}
	switch j.State {
	case "completed", "partial", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (c *Core) DeleteJobs(id string) domain.Result[domain.JobHistoryDeletion] {
	c.gate.Lock()
	if c.stopping {
		c.gate.Unlock()
		return domain.Failure[domain.JobHistoryDeletion](domain.Fail("CONFLICT", "應用程式正在安全停止，未刪除執行紀錄"))
	}
	if id != "" {
		j, ok := c.jobs[id]
		if !ok || (c.hiddenJobs[id] && c.canDeleteJobHistory(j)) {
			c.gate.Unlock()
			return domain.Failure[domain.JobHistoryDeletion](domain.Fail("NOT_FOUND", "執行紀錄已不存在"))
		}
		if !c.canDeleteJobHistory(j) {
			c.gate.Unlock()
			return domain.Failure[domain.JobHistoryDeletion](domain.Fail("CONFLICT", "進行中或待核對作業不能刪除"))
		}
	}
	index := jobHistoryIndex{IDs: []string{}}
	next := map[string]bool{}
	for hidden := range c.hiddenJobs {
		next[hidden] = true
	}
	result := domain.JobHistoryDeletion{}
	for _, j := range c.jobs {
		if id != "" && j.ID != id {
			continue
		}
		if !c.canDeleteJobHistory(j) {
			result.Retained++
		} else if !next[j.ID] {
			next[j.ID] = true
			result.Deleted++
		}
	}
	if result.Deleted == 0 {
		c.gate.Unlock()
		return domain.Success(result)
	}
	for hidden := range next {
		index.IDs = append(index.IDs, hidden)
	}
	sort.Strings(index.IDs)
	if err := c.data.Write(jobHistoryFile, index); err != nil {
		c.gate.Unlock()
		return domain.Failure[domain.JobHistoryDeletion](domain.Fail("STORAGE_ERROR", "執行紀錄列表未能保存；未刪除任何紀錄"))
	}
	c.hiddenJobs = next
	c.gate.Unlock()
	c.event("jobs:changed", nil)
	return domain.Success(result)
}
