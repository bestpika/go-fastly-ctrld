package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

func preflight() error            { return nil }
func startupAlert(message string) { fmt.Fprintln(os.Stderr, message) }
func setLaunchAtLogin(enabled bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(root) {
		root = filepath.Join(home, ".config")
	}
	dir := filepath.Join(root, "autostart")
	path := filepath.Join(dir, "go-fastly-ctrld.desktop")
	if !enabled {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	content, err := desktopEntry(exe)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".go-fastly-ctrld-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.WriteString(content)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func trayHostStatus() trayObservation {
	conn, err := dbus.SessionBus()
	if err != nil {
		return trayObservation{trayUnknown, "無法讀取 D-Bus 工作階段"}
	}
	obj := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	property := func(name string) (dbus.Variant, error) {
		var v dbus.Variant
		err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.kde.StatusNotifierWatcher", name).Store(&v)
		return v, err
	}
	host, err := property("IsStatusNotifierHostRegistered")
	if err != nil {
		return trayObservation{trayUnknown, "無法確認系統匣桌面宿主"}
	}
	registered, ok := host.Value().(bool)
	if !ok {
		return trayObservation{trayUnknown, "系統匣桌面宿主狀態格式無法確認"}
	}
	if !registered {
		return trayObservation{trayAbsent, "桌面未註冊系統匣宿主"}
	}
	items, err := property("RegisteredStatusNotifierItems")
	if err != nil {
		return trayObservation{trayUnknown, "無法讀取系統匣註冊清單"}
	}
	list, ok := items.Value().([]string)
	if !ok {
		return trayObservation{trayUnknown, "系統匣註冊清單格式無法確認"}
	}
	name := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	var owner string
	if err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner); err != nil {
		return trayObservation{trayUnknown, "無法確認本程式的 D-Bus 擁有者"}
	}
	for _, item := range list {
		if strings.HasPrefix(item, name+"/") || item == name || strings.HasPrefix(item, owner+"/") {
			return trayObservation{presence: trayPresent}
		}
	}
	return trayObservation{trayAbsent, "系統匣清單沒有本程式的註冊"}
}
