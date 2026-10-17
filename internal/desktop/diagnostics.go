package desktop

import (
	"encoding/json"

	"github.com/bestpika/go-fastly-ctrld/internal/app"
)

func diagnostics(s app.Snapshot) ([]byte, error) {
	type jobStatus struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		State     string `json:"state"`
		Completed int    `json:"completed"`
		Total     int    `json:"total"`
		Message   string `json:"message"`
	}
	report := struct {
		Version            string            `json:"version"`
		NetworkFingerprint string            `json:"networkFingerprint"`
		NetworkError       string            `json:"networkError"`
		IPv6               bool              `json:"ipv6"`
		Recovery           bool              `json:"recovery"`
		Desktop            app.DesktopStatus `json:"desktop"`
		Jobs               []jobStatus       `json:"jobs"`
	}{Version: s.Version, NetworkFingerprint: s.Network.Fingerprint, NetworkError: s.Network.Error, IPv6: s.Network.IPv6, Recovery: s.Recovery, Desktop: s.Desktop, Jobs: []jobStatus{}}
	for _, j := range s.Jobs {
		report.Jobs = append(report.Jobs, jobStatus{j.ID, j.Kind, j.State, j.Completed, j.Total, j.Message})
	}
	b, err := json.MarshalIndent(report, "", "  ")
	return append(b, '\n'), err
}
