package storage

import (
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func TestCleanupKeepsRecoveryAndLiveCache(t *testing.T) {
	s, _ := Open(t.TempDir())
	now := time.Now()
	_ = s.Write("tests/old.json", domain.TestResult{ExpiresAt: now.Add(-time.Hour)})
	_ = s.Write("tests/new.json", domain.TestResult{ExpiresAt: now.Add(time.Hour)})
	_ = s.Write("references/old.json", domain.SharedReference{ID: "reference", ExpiresAt: now.Add(-time.Hour)})
	_ = s.Write("detections/old.json", domain.DetectionResult{ExpiresAt: now.Add(-time.Hour)})
	_ = s.Write("jobs/pending.json", domain.Job{ID: "pending", Kind: "apply", State: "needs_recovery"})
	_ = s.Write("snapshots/origin.json", domain.Rule{Hostname: "example.com"})
	n, err := s.CleanExpired(now)
	if err != nil || n != 1 {
		t.Fatalf("清理失敗 %d %v", n, err)
	}
	var j domain.Job
	var r domain.Rule
	var result domain.TestResult
	var reference domain.SharedReference
	if s.Read("tests/old.json", &result) != nil || s.Read("references/old.json", &reference) != nil {
		t.Fatal("快取清理因時間刪除測速結果或共享參考")
	}
	if s.Read("jobs/pending.json", &j) != nil || s.Read("snapshots/origin.json", &r) != nil {
		t.Fatal("清理刪除復原資料")
	}
}
