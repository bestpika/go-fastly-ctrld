package desktop

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/app"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type smokeVault struct{}

func (smokeVault) Get() (string, error) { return "", nil }
func (smokeVault) Set(string) error {
	return domain.Fail("KEYRING_UNAVAILABLE", "驗證模式不讀寫系統金鑰庫")
}
func (smokeVault) Delete() error {
	return domain.Fail("KEYRING_UNAVAILABLE", "驗證模式不讀寫系統金鑰庫")
}

type smokeNetwork struct{}

func (smokeNetwork) Snapshot(context.Context) domain.NetworkSnapshot {
	return domain.NetworkSnapshot{Fingerprint: "isolated-smoke-test", ObservedAt: time.Now().UTC(), IPv6Reason: "驗證模式不執行外部連線"}
}

func Smoke(assets fs.FS, root string) error {
	if !filepath.IsAbs(root) {
		return domain.Fail("INVALID_INPUT", "原生驗證必須使用絕對路徑的空目錄")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return domain.Fail("INVALID_INPUT", "原生驗證目錄不存在或不安全")
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		return domain.Fail("INVALID_INPUT", "原生驗證只允許空目錄，不覆寫既有資料")
	}
	return run(assets, false, root)
}

func smokeOptions(root string) app.Options {
	return app.Options{Paths: storage.Paths{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache")}, Vault: smokeVault{}, Network: smokeNetwork{}}
}

type smokeChecks struct {
	closed, recorded     bool
	trayChecks           int
	trayMonitoringPassed bool
}

func saveSmoke(root string, s app.Snapshot, c smokeChecks) error {
	b, err := json.MarshalIndent(struct {
		Version              string `json:"version"`
		UIReady              bool   `json:"uiReady"`
		TrayAvailable        bool   `json:"trayAvailable"`
		AutomaticEnabled     bool   `json:"automaticEnabled"`
		CredentialsSaved     bool   `json:"credentialsSaved"`
		CloseRestored        bool   `json:"closeRestored"`
		ErrorRecorded        bool   `json:"errorRecorded"`
		TrayChecks           int    `json:"trayChecks"`
		TrayMonitoringPassed bool   `json:"trayMonitoringPassed"`
	}{s.Version, true, s.Desktop.TrayAvailable, s.Settings.Automation.Enabled, s.Credentials.Saved, c.closed, c.recorded, c.trayChecks, c.trayMonitoringPassed && c.trayChecks >= 4}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "report.json"), append(b, '\n'), 0600)
}

// 真正送入 Wails 關閉 hook，再恢復同一原生視窗；不以啟動成功代替互動驗證。
func verifyCloseRestore(window *application.WebviewWindow, count *atomic.Uint32) bool {
	wait := func(check func() bool) bool {
		until := time.Now().Add(4 * time.Second)
		for time.Now().Before(until) {
			if check() {
				return true
			}
			time.Sleep(40 * time.Millisecond)
		}
		return false
	}
	previous := count.Load()
	window.Close()
	if !wait(func() bool { return count.Load() > previous && (!window.IsVisible() || window.IsMinimised()) }) {
		return false
	}
	window.Show()
	window.UnMinimise()
	window.Focus()
	return wait(func() bool { return window.IsVisible() && !window.IsMinimised() })
}
