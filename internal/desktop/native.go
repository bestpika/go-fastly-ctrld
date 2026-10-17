package desktop

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/app"
	"github.com/bestpika/go-fastly-ctrld/internal/appicon"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func trayIcon() []byte {
	return appicon.PNG(32)
}

func Run(assets fs.FS, background bool) error {
	return run(assets, background, "")
}

func run(assets fs.FS, background bool, smokeRoot string) error {
	if err := preflight(); err != nil {
		startupAlert(err.Error())
		return err
	}
	bridge := &Bridge{}
	var window *application.WebviewWindow
	var firstWindow atomic.Pointer[application.WebviewWindow]
	var core *app.Core
	var allowQuit, quitting atomic.Bool
	var requestQuit func()
	instanceID := "com.bestpika.GoFastlyControlD"
	if smokeRoot != "" {
		instanceID += ".smoke." + domain.ID()
	}
	a := application.New(application.Options{
		Name: "go-fastly-ctrld", Description: "Fastly 路線最佳化與 Control D 規則同步",
		Icon:     appicon.PNG(256),
		Services: []application.Service{application.NewService(bridge)},
		Assets:   application.AssetOptions{Handler: application.BundledAssetFileServer(assets), DisableLogging: true},
		Windows:  application.WindowsOptions{DisableQuitOnLastWindowClosed: true, WndClass: "GoFastlyControlD"},
		Linux:    application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ProgramName: "go-fastly-ctrld", ApplicationID: "com.bestpika.GoFastlyControlD"},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: instanceID, ExitCode: 0, OnSecondInstanceLaunch: func(application.SecondInstanceData) {
			if w := firstWindow.Load(); w != nil {
				w.Show()
				w.UnMinimise()
				w.Focus()
			}
		}},
		ShouldQuit: func() bool {
			if allowQuit.Load() {
				return true
			}
			if requestQuit != nil {
				requestQuit()
			}
			return false
		},
		OnShutdown: func() {
			if core != nil {
				core.Stop()
			}
		},
	})
	var err error
	options := app.Options{}
	if smokeRoot != "" {
		options = smokeOptions(smokeRoot)
	}
	core, err = app.New(options)
	if err != nil {
		startupAlert("本機資料無法安全開啟；未覆寫設定，請先檢查資料版本與權限")
		return err
	}
	bridge.core = core
	window = a.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", Title: "Fastly 路線最佳化", Width: 1320, Height: 900, MinWidth: 900, MinHeight: 640, URL: "/", Hidden: false, DefaultContextMenuDisabled: true, DevToolsEnabled: false})
	firstWindow.Store(window)
	show := func() { window.Show(); window.UnMinimise(); window.Focus() }
	requestQuit = func() {
		if !quitting.CompareAndSwap(false, true) {
			return
		}
		go func() {
			if core.Stop() {
				allowQuit.Store(true)
				a.Quit()
			} else {
				quitting.Store(false)
				core.RecordFailure("安全退出", domain.Fail("STATE_RECOVERY_REQUIRED", "背景作業尚未完成安全停止，視窗已保留"), "")
				show()
			}
		}()
	}
	bridge.quit = requestQuit
	core.Launch = setLaunchAtLogin
	if smokeRoot != "" {
		core.Launch = func(bool) error { return domain.Fail("DEPENDENCY_MISSING", "驗證模式不修改登入啟動設定") }
	}
	core.SelectFile = func() (string, error) {
		return a.Dialog.OpenFile().SetTitle("匯入網域清單").AddFilter("TXT 或 CSV 清單", "*.txt;*.csv").AttachToWindow(window).PromptForSingleSelection()
	}
	bridge.export = func() error {
		path, err := a.Dialog.SaveFile().SetMessage("匯出遮敏診斷，請選擇新檔案").SetFilename("fastly-diagnostics.json").AddFilter("JSON 診斷", "*.json").AttachToWindow(window).PromptForSingleSelection()
		if err != nil {
			return err
		}
		if path == "" {
			return domain.Fail("CANCELLED", "已取消診斷匯出")
		}
		b, err := diagnostics(core.GetAppSnapshot().Data)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
		}
		return err
	}
	core.Emit = func(name string, e app.Event) { a.Event.Emit(name, e) }
	tray := a.SystemTray.New().SetIcon(trayIcon()).OnClick(show)
	menu := application.NewMenu()
	menu.Add("開啟主視窗").OnClick(func(*application.Context) { show() })
	menu.Add("立即偵測").OnClick(func(*application.Context) { go bridge.StartDetection(nil); show() })
	menu.Add("暫停自動執行").OnClick(func(*application.Context) { go bridge.PauseAutomation() })
	menu.Add("恢復自動執行").OnClick(func(*application.Context) { go bridge.ResumeAutomation() })
	menu.Add("最近結果").OnClick(func(*application.Context) { show(); a.Event.Emit("navigation:requested", "/tests") })
	menu.AddSeparator()
	menu.Add("真正結束程式").OnClick(func(*application.Context) { requestQuit() })
	tray.SetMenu(menu)
	tray.SetTooltip("Fastly 路線最佳化")
	var trayReady atomic.Bool
	var closeEvents atomic.Uint32
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if allowQuit.Load() {
			return
		}
		e.Cancel()
		closeEvents.Add(1)
		closeToBackground(trayReady.Load(), func() { window.Hide() }, func() { window.Minimise() })
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ready sync.Once
	bridge.ready = func() {
		ready.Do(func() {
			go func() {
				time.Sleep(300 * time.Millisecond)
				monitor := trayMonitor{}
				checks := smokeChecks{trayMonitoringPassed: true}
				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()
				for {
					observation := trayHostStatus()
					decision := monitor.observe(observation)
					trayReady.Store(decision.available)
					if decision.changed {
						if !decision.available && !window.IsVisible() {
							window.Show().Minimise()
						}
						core.SetDesktopStatus(app.DesktopStatus{TrayAvailable: decision.available, TrayMessage: decision.message, LaunchAvailable: true, LaunchMessage: "登入啟動由設定開關控制，未核對系統登錄值"})
					}
					if decision.code != "" {
						core.RecordFailure("系統匣監測", domain.Fail(decision.code, decision.message+"；"+observation.reason), "")
					}
					if background && decision.available {
						window.Hide()
						background = false
					} else if monitor.misses >= 3 {
						background = false
					}
					if smokeRoot != "" {
						checks.trayChecks++
						checks.trayMonitoringPassed = checks.trayMonitoringPassed && observation.presence == trayPresent
						if checks.trayChecks == 1 {
							// 空隔離帳號只產生本機前置錯誤，不發外部請求。
							_ = bridge.ReadRuleList(context.Background())
							checks.closed = verifyCloseRestore(window, &closeEvents)
							checks.recorded = len(core.ListErrors().Data) > 0
						}
						if checks.trayChecks >= 4 {
							checks.closed = verifyCloseRestore(window, &closeEvents) && checks.closed
							if err := saveSmoke(smokeRoot, core.GetAppSnapshot().Data, checks); err != nil {
								fmt.Fprintln(os.Stderr, "無法保存原生驗證結果")
							}
							requestQuit()
							return
						}
					}
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
					}
				}
			}()
		})
	}
	core.Start(ctx)
	defer core.Stop()
	if err = a.Run(); err != nil {
		return fmt.Errorf("桌面宿主啟動失敗：%w", err)
	}
	return nil
}
