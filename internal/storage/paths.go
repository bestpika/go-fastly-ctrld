package storage

import (
	"os"
	"path/filepath"
	"runtime"
)

type Paths struct {
	Config string
	Data   string
	Cache  string
}

func UserPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		base = filepath.Join(base, "go-fastly-ctrld")
		return Paths{Config: filepath.Join(base, "config"), Data: filepath.Join(base, "data"), Cache: filepath.Join(base, "cache")}, nil
	}
	xdg := func(key, fallback string) string {
		v := os.Getenv(key)
		if v == "" || !filepath.IsAbs(v) {
			v = filepath.Join(home, fallback)
		}
		return filepath.Join(v, "go-fastly-ctrld")
	}
	return Paths{Config: xdg("XDG_CONFIG_HOME", ".config"), Data: xdg("XDG_DATA_HOME", ".local/share"), Cache: xdg("XDG_CACHE_HOME", ".cache")}, nil
}
