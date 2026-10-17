package desktop

import (
	"strings"
	"testing"
)

func TestAutostartEscaping(t *testing.T) {
	got, err := launchCommand(`C:\Program Files\Fastly\fastly.exe`)
	if err != nil || got != `"C:\Program Files\Fastly\fastly.exe" --background` {
		t.Fatal(got, err)
	}
	if _, err = launchCommand("bad\npath"); err == nil {
		t.Fatal("接受換行")
	}
	got, err = desktopEntry("/home/user/a $b%/fastly")
	if err != nil || !strings.Contains(got, `a \$b%%/fastly`) {
		t.Fatal(got, err)
	}
	if _, err = desktopEntry("bad\nExec=other"); err == nil {
		t.Fatal("接受額外桌面指令")
	}
}
