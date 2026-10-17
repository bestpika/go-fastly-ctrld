package desktop

import (
	"sync"
	"testing"
	"time"
)

func TestCloseDoesNotWaitForApplicationOperations(t *testing.T) {
	for _, operation := range []string{"測速中", "差異預覽後", "API 錯誤後"} {
		t.Run(operation, func(t *testing.T) {
			var applicationLock sync.Mutex
			applicationLock.Lock()
			defer applicationLock.Unlock()
			for _, tray := range []bool{true, false} {
				done := make(chan string, 1)
				go closeToBackground(tray, func() { done <- "hide" }, func() { done <- "minimise" })
				select {
				case action := <-done:
					if (tray && action != "hide") || (!tray && action != "minimise") {
						t.Fatal("關閉動作錯誤")
					}
				case <-time.After(time.Second):
					t.Fatal("關閉被應用作業阻塞")
				}
			}
		})
	}
}
