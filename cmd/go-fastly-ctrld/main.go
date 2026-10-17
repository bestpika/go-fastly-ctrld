package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bestpika/go-fastly-ctrld/frontend"
	"github.com/bestpika/go-fastly-ctrld/internal/desktop"
)

func main() {
	background := flag.Bool("background", false, "啟動至系統匣；系統匣不可用則保留窗口")
	smoke := flag.String("smoke-test", "", "使用指定空目錄驗證原生 IPC，不讀寫真實帳號或金鑰庫")
	flag.Parse()
	if flag.NArg() != 0 || (*background && *smoke != "") {
		fmt.Fprintln(os.Stderr, "啟動參數不正確")
		os.Exit(2)
	}
	var err error
	if *smoke != "" {
		err = desktop.Smoke(frontend.Assets(), *smoke)
	} else {
		err = desktop.Run(frontend.Assets(), *background)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "程式無法安全啟動：", err)
		os.Exit(1)
	}
}
