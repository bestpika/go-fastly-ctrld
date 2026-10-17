package desktop

// 關閉只選原生視窗動作，不讀設定、不等測速／帳號 API／持久化鎖。
func closeToBackground(trayAvailable bool, hide, minimise func()) {
	if trayAvailable {
		hide()
	} else {
		minimise()
	}
}
