package app

import (
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"testing"
	"time"
)

func TestURLConflictsArePrivateAndResultCleanupKeepsRecoveryData(t *testing.T) {
	c, api := newTestCore(t, successfulProbe)
	p := c.PreviewDomainImport("https://cdn.example.com/a.bin", "txt", "")
	if !p.OK || !c.ConfirmDomainImport(p.Data.ID).OK {
		t.Fatal("首次網址匯入失敗")
	}
	p = c.PreviewDomainImport("https://cdn.example.com/b.bin", "txt", "")
	if !p.OK || p.Data.Rows[0].State != "conflict" || c.ConfirmDomainImport(p.Data.ID).OK {
		t.Fatal("衝突默默覆寫")
	}
	r := c.ResolveDomainImport(p.Data.ID, p.Data.Rows[0].Line, true)
	if !r.OK || !c.ConfirmDomainImport(p.Data.ID).OK {
		t.Fatal("明確取代失敗")
	}
	_, ds := c.manager.Snapshot()
	if len(ds) != 1 || ds[0].Hostname != "cdn.example.com" || ds[0].TestURL != "https://cdn.example.com/b.bin" {
		t.Fatal("完整子域或更新錯誤")
	}
	value := domain.TestResult{ID: "test-result", DomainID: ds[0].ID, ObservedAt: time.Now()}
	if err := c.cache.Write("tests/"+ds[0].ID+".json", value); err != nil {
		t.Fatal(err)
	}
	c.results[ds[0].ID] = value
	origin := map[string]string{"safe": "keep"}
	_ = c.data.Write("snapshots/origin-test.json", origin)
	c.testID = "busy"
	if c.DeleteTestResults("").OK {
		t.Fatal("測試中仍可刪除")
	}
	c.testID = ""
	if !c.DeleteTestResults("").OK || len(c.results) != 0 {
		t.Fatal("結果未清除")
	}
	var after map[string]string
	if c.data.Read("snapshots/origin-test.json", &after) != nil || after["safe"] != "keep" {
		t.Fatal("復原資料被清除")
	}
	if api.writes != 0 || api.deletes != 0 {
		t.Fatal("本機清除動到帳號")
	}
}
