package desktop

import (
	"fmt"
	"strings"
)

func launchCommand(path string) (string, error) {
	if strings.ContainsAny(path, "\x00\r\n\"") || path == "" {
		return "", fmt.Errorf("啟動路徑不正確")
	}
	return `"` + path + `" --background`, nil
}

func desktopEntry(path string) (string, error) {
	if strings.ContainsAny(path, "\x00\r\n") || path == "" {
		return "", fmt.Errorf("啟動路徑不正確")
	}
	path = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$", "%", "%%").Replace(path)
	return "[Desktop Entry]\nType=Application\nName=Fastly 路線最佳化\nExec=\"" + path + "\" --background\nTerminal=false\nX-GNOME-Autostart-enabled=true\n", nil
}
