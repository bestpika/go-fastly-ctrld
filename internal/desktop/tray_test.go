package desktop

import "testing"

func TestTrayMonitorKeepsConfirmedTrayDuringTemporaryFailure(t *testing.T) {
	m := trayMonitor{}
	if d := m.observe(trayObservation{presence: trayPresent}); !d.available || !d.changed || d.code != "" {
		t.Fatalf("首次註冊未確認：%+v", d)
	}
	for range 2 {
		if d := m.observe(trayObservation{presence: trayUnknown}); !d.available || d.code != "" {
			t.Fatalf("單次／暫態查詢失敗不應判定不可用：%+v", d)
		}
	}
	if d := m.observe(trayObservation{presence: trayPresent}); !d.available || d.code != "" || m.misses != 0 {
		t.Fatalf("重新確認後未清除暫態：%+v", d)
	}
}

func TestTrayMonitorRetriesClassifiesAndReportsOncePerOutage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		presence trayPresence
		code     string
	}{
		{"未確認不冒稱失效", trayUnknown, "TRAY_UNCONFIRMED"},
		{"連續確認未註冊", trayAbsent, "TRAY_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := trayMonitor{}
			observation := trayObservation{presence: tc.presence}
			for range 2 {
				if d := m.observe(observation); d.available || d.code != "" {
					t.Fatalf("啟動確認期間需保留工作列退路且不發錯誤：%+v", d)
				}
			}
			if d := m.observe(observation); d.available || d.code != tc.code {
				t.Fatalf("三次失敗分類錯誤：%+v", d)
			}
			for range 10 {
				if d := m.observe(observation); d.changed || d.code != "" {
					t.Fatalf("同一中斷不能每五秒重複通知：%+v", d)
				}
			}
			if d := m.observe(trayObservation{presence: trayPresent}); !d.available {
				t.Fatal("重啟桌面宿主後必須能恢復")
			}
			for range 2 {
				if d := m.observe(observation); !d.available || d.code != "" {
					t.Fatalf("恢復後的短暫失敗應保留最近確認：%+v", d)
				}
			}
			if d := m.observe(observation); d.available || d.code != tc.code {
				t.Fatalf("新的持續中斷應重新通知一次：%+v", d)
			}
		})
	}
}

func TestMixedTrayFailuresDoNotClaimConfirmedAbsence(t *testing.T) {
	m := trayMonitor{}
	m.observe(trayObservation{presence: trayPresent})
	m.observe(trayObservation{presence: trayAbsent})
	m.observe(trayObservation{presence: trayUnknown})
	if d := m.observe(trayObservation{presence: trayAbsent}); d.available || d.code != "TRAY_UNCONFIRMED" {
		t.Fatalf("只有一次確認未註冊，不能宣稱已連續確認失效：%+v", d)
	}
}
