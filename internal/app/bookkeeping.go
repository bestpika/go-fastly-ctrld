package app

import (
	"errors"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

type automaticCheckpoint struct {
	Blocked bool   `json:"blocked"`
	Message string `json:"message"`
}

func (c *Core) loadAutomaticCheckpoint() error {
	var checkpoint automaticCheckpoint
	if err := c.data.Read("automation-checkpoint.json", &checkpoint); err != nil {
		if errors.Is(err, storage.ErrMissing) {
			return nil
		}
		return domain.Fail("SCHEMA_UNSUPPORTED", "自動暫停紀錄未知或損毀；未繼續啟動")
	}
	if checkpoint.Blocked {
		c.automationError = "上次自動作業中斷，請核對結果並重新授權"
	}
	return nil
}

func (c *Core) armAutomaticCheckpoint() error {
	if err := c.data.Write("automation-checkpoint.json", automaticCheckpoint{Blocked: true, Message: "自動作業安全紀錄尚未完成"}); err != nil {
		c.blockAutomatic("自動前置紀錄無法保存；未開始新的自動作業")
		return domain.Fail("STORAGE_ERROR", "自動前置紀錄無法安全保存")
	}
	return nil
}

func (c *Core) blockAutomatic(message string) {
	c.gate.Lock()
	c.automationError = message
	c.automationGeneration++
	if c.testCancel != nil {
		c.testCancel()
	}
	if c.writerCancel != nil {
		c.writerCancel()
	}
	c.gate.Unlock()
	// 已開始的自動作業有前置標記；其他故障也盡可能保存暫停狀態。
	if err := c.data.Write("automation-checkpoint.json", automaticCheckpoint{Blocked: true, Message: message}); err != nil {
		var previous automaticCheckpoint
		if c.data.Read("automation-checkpoint.json", &previous) != nil || !previous.Blocked {
			c.gate.Lock()
			c.automationError += "；暫停標記無法保存，請先修復資料目錄再退出"
			c.gate.Unlock()
		}
	}
	c.event("automation:changed", nil)
	c.RecordFailure("自動執行安全暫停", domain.Fail("AUTOMATION_PAUSED", message), "")
}

func (c *Core) clearAutomaticCheckpoint(generation uint64, acknowledge bool) error {
	c.gate.Lock()
	if c.automationGeneration != generation || (!acknowledge && c.automationError != "") {
		c.gate.Unlock()
		return domain.Fail("STORAGE_ERROR", "安全暫停狀態已變更，請重新核對")
	}
	if err := c.data.Write("automation-checkpoint.json", automaticCheckpoint{}); err != nil {
		c.gate.Unlock()
		c.blockAutomatic("自動作業紀錄無法完成；自動執行已暫停")
		return domain.Fail("STORAGE_ERROR", "自動暫停紀錄無法安全更新")
	}
	c.automationError = ""
	c.automationGeneration++
	c.gate.Unlock()
	c.event("automation:changed", nil)
	return nil
}

func automaticHistory(s domain.Settings, jobs map[string]domain.Job) domain.Automation {
	a := copyOf(s.Automation)
	if a.ProfileID != s.ProfileID || a.CredentialRevision != s.CredentialRevision {
		a.LastRun = time.Time{}
		a.LastChanged = map[string]time.Time{}
	}
	if a.LastChanged == nil {
		a.LastChanged = map[string]time.Time{}
	}
	for _, j := range jobs {
		if j.Trigger != "automatic" || j.ProfileID != s.ProfileID || j.CredentialRevision != s.CredentialRevision {
			continue
		}
		if j.StartedAt.After(a.LastRun) {
			a.LastRun = j.StartedAt
		}
		if j.Kind != "apply" {
			continue
		}
		at := j.FinishedAt
		if at.IsZero() {
			at = j.StartedAt
		}
		for _, item := range j.Items {
			id := item.Preview.DomainID
			if item.State == "verified" && id != "" && at.After(a.LastChanged[id]) {
				a.LastChanged[id] = at
			}
		}
	}
	return a
}

func (c *Core) reconcileAutomaticHistory() error {
	c.gate.Lock()
	defer c.gate.Unlock()
	if c.stopping || c.writerID != "" || c.recovery {
		return domain.Fail("STATE_RECOVERY_REQUIRED", "請先核對未決作業")
	}
	s, _ := c.manager.Snapshot()
	a := automaticHistory(s, c.jobs)
	if domain.Digest(a) == domain.Digest(s.Automation) {
		return nil
	}
	if err := c.manager.RecordAutomation(a, s.Cursor); err != nil {
		return domain.Fail("STORAGE_ERROR", "無法從已驗證作業重建排程紀錄")
	}
	return nil
}

func nextAutomaticCursor(cursor, completed, count int) int {
	if count < 1 {
		return 0
	}
	step := completed
	if step < 1 || step >= count {
		step = 1
	}
	return (cursor + step) % count
}

func (c *Core) finishAutomaticTest(j *domain.Job, captured domain.Settings, count int) bool {
	c.gate.Lock()
	blocked := c.automationError != ""
	generation := c.automationGeneration
	c.gate.Unlock()
	if blocked {
		return false
	}
	s, _ := c.manager.Snapshot()
	if s.ProfileID == captured.ProfileID && s.CredentialRevision == captured.CredentialRevision {
		a := s.Automation
		if j.StartedAt.After(a.LastRun) {
			a.LastRun = j.StartedAt
		}
		completed := j.ManagedCompleted
		if count == 0 {
			count = len(probe.SelectedBaselines(captured.Probe, nil))
			completed = j.BaselinesCompleted
		}
		if err := c.manager.RecordAutomation(a, nextAutomaticCursor(captured.Cursor, completed, count)); err != nil {
			c.blockAutomatic("排程紀錄保存失敗，未自動套用；請重新確認授權")
			j.Message = "排程紀錄保存失敗，未自動套用"
			_ = c.saveJob(*j)
			return false
		}
	}
	return c.clearAutomaticCheckpoint(generation, false) == nil
}
