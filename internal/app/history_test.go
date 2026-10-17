package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

func reopenHistoryCore(t *testing.T, c *Core, api *testAPI) (*Core, error) {
	t.Helper()
	next, err := New(Options{Paths: storage.Paths{Config: c.config.Root, Data: c.data.Root, Cache: c.cache.Root}, Vault: &testVault{}, Network: testNetwork{}, API: api})
	if err == nil {
		t.Cleanup(func() { next.Stop() })
	}
	return next, err
}

func TestDeleteJobsPersistsAndPreservesSafetyData(t *testing.T) {
	c, api := newTestCore(t, nil)
	s := c.GetAppSnapshot().Data.Settings
	now := time.Now().UTC()
	j := domain.Job{ID: "history", Kind: "apply", Trigger: "automatic", State: "completed", StartedAt: now.Add(-time.Minute), FinishedAt: now, ProfileID: s.ProfileID, CredentialRevision: s.CredentialRevision, Items: []domain.JournalItem{{State: "verified", Preview: domain.PreviewItem{DomainID: "domain"}}}}
	if err := c.saveJob(j); err != nil {
		t.Fatal(err)
	}
	result := domain.TestResult{ID: "result", DomainID: "domain", ExpiresAt: now.Add(time.Hour)}
	c.results[result.DomainID] = result
	if err := c.cache.Write("tests/domain.json", result); err != nil {
		t.Fatal(err)
	}
	if err := c.data.Write("snapshots/origin-test.json", map[string]string{"original": "keep"}); err != nil {
		t.Fatal(err)
	}
	if err := c.data.Write("automation-checkpoint.json", automaticCheckpoint{Blocked: true}); err != nil {
		t.Fatal(err)
	}
	c.RecordFailure("測試", domain.Fail("TEST_ERROR", "保留錯誤"), j.ID)
	journalPath := filepath.Join(c.data.Root, "jobs", j.ID+".json")
	before, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	events := 0
	c.Emit = func(name string, event Event) {
		if name == "jobs:changed" && event.Job == nil {
			events++
		}
	}
	r := c.DeleteJobs(j.ID)
	if !r.OK || r.Data.Deleted != 1 || r.Data.Retained != 0 || events != 1 || len(c.ListJobs().Data) != 0 || len(c.GetAppSnapshot().Data.Jobs) != 0 || !c.GetJob(j.ID).OK {
		t.Fatalf("列表未同步移除或安全底帳遺失：%+v", r)
	}
	after, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("列表刪除改動原始 journal")
	}
	next, err := reopenHistoryCore(t, c, api)
	if err != nil {
		t.Fatal(err)
	}
	snap := next.GetAppSnapshot().Data
	if len(snap.Jobs) != 0 || len(snap.Results) != 1 || len(next.ListErrors().Data) != 1 || snap.AutomationError == "" || domain.Digest(snap.Settings) != domain.Digest(s) {
		t.Fatal("重開後列表復現或其他資料被刪除")
	}
	var origin map[string]string
	if err := next.data.Read("snapshots/origin-test.json", &origin); err != nil || origin["original"] != "keep" {
		t.Fatal("復原原像遺失")
	}
	a := automaticHistory(s, next.jobs)
	if !a.LastChanged["domain"].Equal(now) || !a.LastRun.Equal(j.StartedAt) {
		t.Fatal("已移除列表的 journal 不再參與自動冷卻重建")
	}
	if r := next.DeleteJobs(j.ID); r.OK || r.Error.Code != "NOT_FOUND" {
		t.Fatal("重複刪除沒有回報已不存在")
	}
	if r := next.DeleteJobs(""); !r.OK || r.Data.Deleted != 0 || r.Data.Retained != 0 {
		t.Fatal("空列表清除未保持冪等")
	}
	if api.writes != 0 || api.deletes != 0 {
		t.Fatal("刪除列表發出遠端寫入")
	}
}

func TestDeleteJobsProtectsActivePendingAndUnknownStates(t *testing.T) {
	for _, tc := range []struct {
		name, state, item, slot string
	}{
		{name: "排隊", state: "queued"}, {name: "執行中", state: "running"},
		{name: "待核對", state: "needs_recovery"}, {name: "未知狀態", state: "future"},
		{name: "終態仍有未知寫入", state: "partial", item: "unknown"},
		{name: "終態仍有寫入", state: "completed", item: "write_started"},
		{name: "測試槽尚未釋放", state: "completed", slot: "test"},
		{name: "寫入槽尚未釋放", state: "failed", slot: "writer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestCore(t, nil)
			j := domain.Job{ID: "protected", Kind: "test", State: tc.state}
			if tc.item != "" {
				j.Items = []domain.JournalItem{{State: tc.item}}
			}
			if err := c.saveJob(j); err != nil {
				t.Fatal(err)
			}
			if tc.slot == "test" {
				c.testID = j.ID
			} else if tc.slot == "writer" {
				c.writerID = j.ID
			}
			if r := c.DeleteJobs(j.ID); r.OK || r.Error.Code != "CONFLICT" {
				t.Fatalf("保護作業可單筆刪除：%+v", r)
			}
			if r := c.DeleteJobs(""); !r.OK || r.Data.Deleted != 0 || r.Data.Retained != 1 || len(c.ListJobs().Data) != 1 {
				t.Fatalf("全清未保留保護作業：%+v", r)
			}
		})
	}
}

func TestDeleteAllJobsClearsTerminalAndKeepsRecovery(t *testing.T) {
	c, api := newTestCore(t, nil)
	for _, state := range []string{"completed", "partial", "failed", "cancelled", "running", "needs_recovery"} {
		if err := c.saveJob(domain.Job{ID: state, Kind: "test", State: state}); err != nil {
			t.Fatal(err)
		}
	}
	c.testID = "running"
	r := c.DeleteJobs("")
	if !r.OK || r.Data.Deleted != 4 || r.Data.Retained != 2 || len(c.ListJobs().Data) != 2 || !c.GetAppSnapshot().Data.Recovery || c.testID != "running" || len(c.jobs) != 6 {
		t.Fatalf("全清誤刪保護作業或解除復原：%+v", r)
	}
	// 舊索引即使意外含待核對 ID，也不能把安全警示藏起來。
	c.hiddenJobs["needs_recovery"] = true
	if err := c.data.Write(jobHistoryFile, jobHistoryIndex{IDs: []string{"completed", "partial", "failed", "cancelled", "needs_recovery"}}); err != nil {
		t.Fatal(err)
	}
	next, err := reopenHistoryCore(t, c, api)
	if err != nil || !next.GetAppSnapshot().Data.Recovery {
		t.Fatalf("重開後未保留待核對狀態：%v", err)
	}
	found := false
	for _, j := range next.ListJobs().Data {
		found = found || j.ID == "needs_recovery"
	}
	if !found {
		t.Fatal("列表索引遮蔽了待核對作業")
	}
}

func TestDeleteJobsStorageFailureDoesNotChangeList(t *testing.T) {
	c, _ := newTestCore(t, nil)
	if err := c.saveJob(domain.Job{ID: "done", Kind: "test", State: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := c.data.Write(jobHistoryFile, jobHistoryIndex{IDs: []string{}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.data.Root, jobHistoryFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	events := 0
	c.Emit = func(string, Event) { events++ }
	c.data.BeforeReplace = func() error { return errors.New("模擬原子保存失敗") }
	defer func() { c.data.BeforeReplace = nil }()
	r := c.DeleteJobs("")
	after, err := os.ReadFile(path)
	if r.OK || r.Error.Code != "STORAGE_ERROR" || err != nil || !bytes.Equal(before, after) || len(c.ListJobs().Data) != 1 || len(c.hiddenJobs) != 0 || events != 0 {
		t.Fatal("保存失敗仍刪除列表、修改索引或發出成功事件")
	}
}

func TestUnknownJobHistoryIndexIsNeverOverwritten(t *testing.T) {
	for _, content := range []string{
		`{"version":1,"data":null}`,
		`{"version":1,"data":{}}`,
		`{"version":99,"data":{"ids":[]}}`,
		`{"version":1,"data":{"ids":[],"future":true}}`,
		`{"version":1,"data":{"ids":[""]}}`,
		`{"version":1,"data":{"ids":["duplicate","duplicate"]}}`,
	} {
		t.Run(content, func(t *testing.T) {
			c, api := newTestCore(t, nil)
			path := filepath.Join(c.data.Root, jobHistoryFile)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := reopenHistoryCore(t, c, api); err == nil {
				t.Fatal("未知或損毀索引仍繼續啟動")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != content {
				t.Fatal("未知索引被覆寫")
			}
		})
	}
}

func TestDeleteJobsMissingAndStopping(t *testing.T) {
	c, _ := newTestCore(t, nil)
	if r := c.DeleteJobs("../jobs"); r.OK || r.Error.Code != "NOT_FOUND" {
		t.Fatal("不存在的 ID 沒有拒絕")
	}
	c.stopping = true
	if r := c.DeleteJobs(""); r.OK || r.Error.Code != "CONFLICT" {
		t.Fatal("停止期間仍清除列表")
	}
}
