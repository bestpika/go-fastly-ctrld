package desktop

import (
	"testing"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

func TestTrayRegistrationProbeDoesNotAlterOrCreateIcons(t *testing.T) {
	for _, registered := range []bool{false, true} {
		calls := 0
		got := probeTrayRegistration(w32.HWND(42), func(command uintptr, n *w32.NOTIFYICONDATA) bool {
			calls++
			want := w32.NOTIFYICONDATA{HWnd: 42, UID: 1}
			want.CbSize = uint32(unsafe.Sizeof(want))
			if command != w32.NIM_MODIFY || *n != want {
				t.Fatalf("只能核對自身 HWND／UID，不能改圖示、提示、可見性或版本：%d %+v", command, n)
			}
			return registered
		})
		if got != registered || calls != 1 {
			t.Fatalf("必須採用註冊探測結果，不可假定桌面宿主存在即成功：%v %d", got, calls)
		}
	}
}
