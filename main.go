package main

import (
	"cczjVideo/app/handoff"
	"os"
)

func main() {
	// 重启交接必须排在建应用之前：application.New 一拿到单实例锁就会把「第二实例」
	// 直接 os.Exit，等不到后面的代码。
	handoff.Await(os.Args[1:])
	app := buildApp()
	if err := app.Run(); err != nil {
		panic(err)
	}
}
