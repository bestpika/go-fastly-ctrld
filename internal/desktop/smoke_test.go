package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bestpika/go-fastly-ctrld/internal/app"
)

func TestSmokeRejectsExistingDataBeforeOpeningDesktop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("原檔"), 0600); err != nil {
		t.Fatal(err)
	}
	if Smoke(nil, root) == nil {
		t.Fatal("驗證模式接受非空目錄")
	}
	b, _ := os.ReadFile(filepath.Join(root, "existing.txt"))
	if string(b) != "原檔" {
		t.Fatal("修改既有資料")
	}
}

func TestSmokeVaultNeverStoresSecret(t *testing.T) {
	v := smokeVault{}
	if v.Set("secret") == nil || v.Delete() == nil {
		t.Fatal("驗證模式不應允許金鑰操作")
	}
	if token, err := v.Get(); token != "" || err != nil {
		t.Fatal("驗證模式讀到權杖")
	}
}

func TestSmokeReportContainsActualSnapshotVersion(t *testing.T) {
	root := t.TempDir()
	if err := saveSmoke(root, app.Snapshot{Version: "2026.290.1"}, smokeChecks{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &report) != nil || report.Version != "2026.290.1" {
		t.Fatal("隔離報告未保留實際執行版本")
	}
}

func TestSmokeMonitoringRequiresFourSuccessfulChecks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks smokeChecks
		passed bool
	}{
		{"快速啟動不能代替持續監測", smokeChecks{trayChecks: 1, trayMonitoringPassed: true}, false},
		{"四次確認才通過", smokeChecks{closed: true, recorded: true, trayChecks: 4, trayMonitoringPassed: true}, true},
		{"中途失敗不能當穩定", smokeChecks{trayChecks: 4, trayMonitoringPassed: false}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := saveSmoke(root, app.Snapshot{}, tc.checks); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(root, "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report struct {
				TrayChecks           int  `json:"trayChecks"`
				TrayMonitoringPassed bool `json:"trayMonitoringPassed"`
				CloseRestored        bool `json:"closeRestored"`
				ErrorRecorded        bool `json:"errorRecorded"`
			}
			if err := json.Unmarshal(b, &report); err != nil {
				t.Fatal(err)
			}
			if report.TrayChecks != tc.checks.trayChecks || report.TrayMonitoringPassed != tc.passed || report.CloseRestored != tc.checks.closed || report.ErrorRecorded != tc.checks.recorded {
				t.Fatalf("監測報告不符實際驗證：%+v", report)
			}
		})
	}
}
