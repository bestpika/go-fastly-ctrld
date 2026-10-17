package network

import (
	"context"
	"testing"
)

func TestLocalSnapshotDoesNotNeedExternalIPv6(t *testing.T) {
	i := New()
	i.CheckIPv6 = func(context.Context) bool { return false }
	a, b := i.Snapshot(context.Background()), i.Snapshot(context.Background())
	if a.Fingerprint == "" || a.Fingerprint != b.Fingerprint || a.IPv6 {
		t.Fatal("網路指紋不穩定")
	}
}
