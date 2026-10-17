package version

import "testing"

func TestPinnedVersions(t *testing.T) {
	if Wails != "v3.0.0-beta.28" || len(Sakai) != 40 || len(SakaiAssets) != 40 {
		t.Fatal("版本鎖定資訊不完整")
	}
}
