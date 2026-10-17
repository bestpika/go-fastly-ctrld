package rulesync

import (
	"context"
	"errors"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/dns"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

type API interface {
	Rules(context.Context, string) (map[string]domain.Rule, []domain.Group, error)
	Write(context.Context, string, domain.Rule, bool) error
	Delete(context.Context, string, string) error
}
type Engine struct {
	API      API
	Store    *storage.Store
	Validate func(context.Context, domain.SyncPreview, domain.PreviewItem) error
	Changed  func(domain.Job)
}
type Origin struct {
	ProfileID  string       `json:"profileId"`
	Hostname   string       `json:"hostname"`
	Before     *domain.Rule `json:"before"`
	CapturedAt time.Time    `json:"capturedAt"`
}

func (e *Engine) save(j domain.Job) error {
	if err := e.Store.Write("jobs/"+j.ID+".json", j); err != nil {
		return err
	}
	if e.Changed != nil {
		e.Changed(j)
	}
	return nil
}
func (e *Engine) origin(p domain.SyncPreview, i domain.PreviewItem) error {
	path := "snapshots/origin-" + domain.Digest([]string{p.ProfileID, i.Hostname}) + ".json"
	var old Origin
	err := e.Store.Read(path, &old)
	if err == nil {
		if old.ProfileID != p.ProfileID || old.Hostname != i.Hostname {
			return domain.Fail("SCHEMA_UNSUPPORTED", "首次接管快照不一致")
		}
		return nil
	}
	if !errors.Is(err, storage.ErrMissing) {
		return err
	}
	return e.Store.Write(path, Origin{ProfileID: p.ProfileID, Hostname: i.Hostname, Before: i.Before, CapturedAt: time.Now().UTC()})
}

func beforeMatches(item domain.PreviewItem, rules map[string]domain.Rule) bool {
	r, ok := rules[item.Hostname]
	if item.Before == nil {
		return !ok
	}
	return ok && r.ObservationDigest() == item.BeforeDigest
}
func afterMatches(item domain.PreviewItem, rules map[string]domain.Rule) bool {
	r, ok := rules[item.Hostname]
	if item.After == nil {
		return !ok
	}
	return ok && r.SemanticDigest() == item.After.SemanticDigest() && (item.Before == nil || r.Safe)
}

func sameTarget(item domain.PreviewItem) bool {
	if item.Before == nil || item.After == nil {
		return item.Before == nil && item.After == nil
	}
	return item.Before.SemanticDigest() == item.After.SemanticDigest()
}

func (e *Engine) Apply(ctx context.Context, p domain.SyncPreview, j domain.Job) domain.Job {
	if j.ID == "" {
		j.ID = domain.ID()
	}
	j.Kind = p.Kind
	j.ProfileID = p.ProfileID
	j.CredentialRevision = p.CredentialRevision
	j.StartedAt = time.Now().UTC()
	j.State = "running"
	j.Total = len(p.Items)
	j.Items = []domain.JournalItem{}
	for _, i := range p.Items {
		state := "prepared"
		if !i.Applicable {
			state = "skipped"
		}
		j.Items = append(j.Items, domain.JournalItem{Preview: i, State: state, Message: i.Reason})
	}
	if e.save(j) != nil {
		j.State = "failed"
		j.Message = "無法保存作業；未執行寫入"
		return j
	}
	for index := range j.Items {
		item := &j.Items[index]
		if !item.Preview.Applicable {
			j.Completed++
			continue
		}
		if ctx.Err() != nil {
			item.State = "cancelled"
			item.Message = "尚未開始此項寫入"
			continue
		}
		if e.Validate != nil {
			if err := e.Validate(ctx, p, item.Preview); err != nil {
				item.State = "failed"
				item.Message = domain.Failure[bool](err).Error.Message
				j.Completed++
				continue
			}
		}
		rules, groups, err := e.API.Rules(ctx, p.ProfileID)
		if err != nil {
			item.State = "failed"
			item.Message = "寫入前回讀失敗；未修改"
			j.Completed++
			continue
		}
		if !beforeMatches(item.Preview, rules) {
			item.State = "conflict"
			item.Message = "規則已被其他來源變更；未覆蓋"
			j.Completed++
			continue
		}
		if p.Kind == "apply" && item.Preview.After != nil && !domain.RegularFolder(groups, item.Preview.After.Group) {
			item.State = "conflict"
			item.Message = "目的資料夾已變更；未寫入"
			j.Completed++
			continue
		}
		if item.Preview.OriginalDNS != nil {
			if err = dns.ValidateOriginalRules(item.Preview.Hostname, *item.Preview.OriginalDNS, rules); err != nil {
				item.State = "conflict"
				item.Message = domain.Failure[bool](err).Error.Message
				j.Completed++
				continue
			}
		}
		if err = e.origin(p, item.Preview); err != nil {
			item.State = "failed"
			item.Message = "首次接管快照無法安全保存"
			j.Completed++
			continue
		}
		item.State = "write_started"
		item.Message = "寫入意圖已持久化，正在提交並回讀"
		if e.save(j) != nil {
			j.State = "needs_recovery"
			j.Message = "寫入前 journal 保存失敗；未送出此項請求"
			return j
		}
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		if item.Preview.After == nil {
			err = e.API.Delete(writeCtx, p.ProfileID, item.Preview.Hostname)
		} else {
			err = e.API.Write(writeCtx, p.ProfileID, *item.Preview.After, item.Preview.Before == nil)
		}
		if err == nil {
			item.WriteAccepted = true
			if e.save(j) != nil {
				cancel()
				j.State = "needs_recovery"
				j.Message = "成功回應保存失敗，須核對；不重送"
				return j
			}
		}
		rules, _, readErr := e.API.Rules(writeCtx, p.ProfileID)
		cancel()
		if readErr != nil {
			item.State = "unknown"
			item.Message = "寫入後無法回讀；須先核對，不重送"
		} else if afterMatches(item.Preview, rules) && sameTarget(item.Preview) && !item.WriteAccepted {
			item.State = "unconfirmed"
			item.Message = "API 未確認同值寫入，不重送"
		} else if afterMatches(item.Preview, rules) {
			item.State = "verified"
			item.Message = "API 回讀一致"
			if actual, ok := rules[item.Preview.Hostname]; ok {
				item.After = &actual
			}
		} else if beforeMatches(item.Preview, rules) && err != nil {
			item.State = "failed"
			item.Message = "寫入失敗且原值仍在；未重送"
		} else {
			item.State = "unknown"
			item.Message = "回讀與預期不同，待核對"
		}
		j.Completed++
		if e.save(j) != nil {
			j.State = "needs_recovery"
			j.Message = "寫入後 journal 保存失敗，保留未決寫入記錄"
			return j
		}
		if item.State == "unknown" {
			break
		}
	}
	finish(&j, ctx.Err() != nil)
	if e.save(j) != nil {
		j.State = "needs_recovery"
		j.Message = "最終作業狀態無法保存，須先核對"
	}
	return j
}

func finish(j *domain.Job, cancelled bool) {
	j.FinishedAt = time.Now().UTC()
	verified, bad, skipped := 0, 0, 0
	for _, i := range j.Items {
		if i.State == "verified" {
			verified++
		} else if i.State == "skipped" {
			skipped++
		} else {
			bad++
		}
	}
	if j.Pending() {
		j.State = "needs_recovery"
		j.Message = "有未決寫入，必須核對後才能開始其他寫入"
	} else if cancelled && verified == 0 {
		j.State = "cancelled"
	} else if bad > 0 && verified > 0 {
		j.State = "partial"
	} else if bad > 0 {
		j.State = "failed"
	} else {
		j.State = "completed"
		if verified == 0 && skipped > 0 {
			j.Message = "沒有可套用項目，帳號未修改"
		}
	}
}

func (e *Engine) Recover(ctx context.Context, j domain.Job) (domain.Job, error) {
	rules, _, err := e.API.Rules(ctx, j.ProfileID)
	if err != nil {
		return j, err
	}
	for index := range j.Items {
		item := &j.Items[index]
		if item.State != "write_started" && item.State != "unknown" {
			continue
		}
		if afterMatches(item.Preview, rules) && sameTarget(item.Preview) && !item.WriteAccepted {
			item.State = "unconfirmed"
			item.Message = "缺少 API 成功證據，不重送"
		} else if afterMatches(item.Preview, rules) {
			item.State = "verified"
			item.Message = "恢復核對後確認目標值"
			if r, ok := rules[item.Preview.Hostname]; ok {
				item.After = &r
			}
		} else if beforeMatches(item.Preview, rules) {
			item.State = "failed"
			item.Message = "恢復核對後確認原值，未重送"
		} else {
			item.State = "conflict"
			item.Message = "遠端已有其他值；停止覆寫並保留快照"
		}
	}
	finish(&j, false)
	if err = e.save(j); err != nil {
		return j, err
	}
	return j, nil
}
