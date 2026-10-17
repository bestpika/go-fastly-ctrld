package desktop

import (
	"errors"
	"strings"
	"testing"

	"github.com/bestpika/go-fastly-ctrld/internal/app"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

func TestExportCancelAndPrivateError(t *testing.T) {
	b := Bridge{export: func() error { return domain.Fail("CANCELLED", "已取消匯出") }}
	result := b.ExportDiagnostics()
	if result.OK || result.Error.Code != "CANCELLED" {
		t.Fatal("取消匯出誤報儲存故障")
	}
	b.export = func() error { return errors.New("private-secret-and-path") }
	result = b.ExportDiagnostics()
	if result.Error.Code != "STORAGE_ERROR" || strings.Contains(result.Error.Message, "private-secret") {
		t.Fatal("私密儲存錯誤未遮蔽")
	}
}

func TestDeleteJobsBridgeReturnsCountsAndRecordsProtectedFailure(t *testing.T) {
	options := smokeOptions(t.TempDir())
	data, err := storage.Open(options.Paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"completed", "needs_recovery"} {
		if err := data.Write("jobs/"+state+".json", domain.Job{ID: state, Kind: "apply", State: state}); err != nil {
			t.Fatal(err)
		}
	}
	core, err := app.New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Stop() })
	b := Bridge{core: core}
	r := b.DeleteJobs("")
	if !r.OK || r.Data.Deleted != 1 || r.Data.Retained != 1 || len(b.ListJobs().Data) != 1 || !b.GetJob("completed").OK {
		t.Fatalf("橋接未保留刪除計數或安全底帳：%+v", r)
	}
	r = b.DeleteJobs("needs_recovery")
	if r.OK || r.Error.Code != "CONFLICT" || r.Error.ErrorID == "" || len(b.ListErrors().Data) != 1 {
		t.Fatal("保護作業的拒絕未透過統一錯誤紀錄回傳")
	}
}
