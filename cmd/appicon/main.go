package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bestpika/go-fastly-ctrld/internal/appicon"
)

func main() {
	out := flag.String("out", "", "ICO 輸出路徑，不覆寫既有檔案")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "請指定 -out 輸出路徑")
		os.Exit(1)
	}
	if err := writeIcon(*out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeIcon(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(appicon.ICO())
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
