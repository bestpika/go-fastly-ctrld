package desktop

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/wailsapp/go-webview2/webviewloader"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func preflight() error {
	if _, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString(""); err != nil {
		return fmt.Errorf("請先安裝 Microsoft Edge WebView2 Runtime")
	}
	return nil
}
func startupAlert(message string) {
	t, _ := windows.UTF16PtrFromString("Fastly 路線最佳化")
	m, _ := windows.UTF16PtrFromString(message)
	_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}
func setLaunchAtLogin(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		err = k.DeleteValue("GoFastlyControlD")
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	path, err := os.Executable()
	if err != nil {
		return err
	}
	command, err := launchCommand(path)
	if err != nil {
		return err
	}
	return k.SetStringValue("GoFastlyControlD", command)
}
func trayHostStatus() trayObservation {
	// 固定 Wails beta.28 的單一 SystemTray 為 ID 1；只核對自己 PID 的
	// message-only window。隱藏區圖示仍為已註冊，不能以座標判定可用性。
	u := windows.NewLazySystemDLL("user32.dll")
	name, _ := windows.UTF16PtrFromString("GoFastlyControlD")
	var after uintptr
	for {
		h, _, _ := u.NewProc("FindWindowExW").Call(^uintptr(2), after, uintptr(unsafe.Pointer(name)), 0)
		if h == 0 {
			return trayObservation{trayUnknown, "Windows 尚未確認本程式的系統匣註冊"}
		}
		after = h
		var pid uint32
		_, _, _ = u.NewProc("GetWindowThreadProcessId").Call(h, uintptr(unsafe.Pointer(&pid)))
		if pid != uint32(os.Getpid()) {
			continue
		}
		if probeTrayRegistration(w32.HWND(h), w32.ShellNotifyIcon) {
			return trayObservation{presence: trayPresent}
		}
	}
}

func probeTrayRegistration(hwnd w32.HWND, notify func(uintptr, *w32.NOTIFYICONDATA) bool) bool {
	// NIM_MODIFY 只核對既有身分。UFlags 為零，不改圖示、提示、可見性、
	// 版本或選單，也不新增／刪除圖示；沒有依賴 Shell_NotifyIconGetRect。
	n := w32.NOTIFYICONDATA{HWnd: hwnd, UID: 1}
	n.CbSize = uint32(unsafe.Sizeof(n))
	return notify(w32.NIM_MODIFY, &n)
}
