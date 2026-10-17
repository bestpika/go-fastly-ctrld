package desktop

// 座標或單次查詢失敗不能證明圖示不存在；暫態與確認不存在分開處理。
type trayPresence uint8

const (
	trayUnknown trayPresence = iota
	trayPresent
	trayAbsent
)

type trayObservation struct {
	presence trayPresence
	reason   string
}

type trayStatus struct {
	available bool
	message   string
}

type trayDecision struct {
	trayStatus
	changed bool
	code    string
}

type trayMonitor struct {
	status   trayStatus
	misses   int
	absences int
	reported bool
}

func (m *trayMonitor) observe(o trayObservation) trayDecision {
	previous := m.status
	code := ""
	if o.presence == trayPresent {
		m.misses, m.absences, m.reported = 0, 0, false
		m.status = trayStatus{true, "關閉視窗將縮到系統匣"}
	} else {
		m.misses = min(m.misses+1, 3)
		if o.presence == trayAbsent {
			m.absences = min(m.absences+1, 3)
		} else {
			m.absences = 0
		}
		if m.misses < 3 {
			if m.status.available {
				m.status.message = "系統匣確認中，沿用上次狀態"
			} else {
				m.status.message = "系統匣確認中，關閉將最小化"
			}
		} else {
			m.status = trayStatus{false, "無法確認系統匣，關閉將最小化"}
			code = "TRAY_UNCONFIRMED"
			if m.absences >= 3 {
				m.status.message = "系統匣不可用，關閉將最小化"
				code = "TRAY_UNAVAILABLE"
			}
			if m.reported {
				code = ""
			}
			m.reported = true
		}
	}
	return trayDecision{trayStatus: m.status, changed: previous != m.status, code: code}
}
