package main

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/handoff"
	"fmt"
	"os"
)

func main() {
	// 重启交接必须排在建应用之前：application.New 一拿到单实例锁就会把「第二实例」
	// 直接 os.Exit，等不到后面的代码。
	handoff.Await(os.Args[1:])
	app, err := buildApp()
	if err != nil {
		reportApplicationFailure(err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		reportApplicationFailure(err)
		os.Exit(1)
	}
}

// Failure can precede the WebView or follow its teardown. The native reporter
// therefore cannot depend on Wails dispatch or on a working database.
func reportApplicationFailure(err error) {
	id := apperror.NewRequestID()
	public := apperror.Public(err)
	applog.ErrorFields("application failed", applog.Fields{"request_id": id, "code": public.Code, "error": apperror.Diagnostic(err)})
	applog.Default().Close()
	message := fmt.Sprintf("%s\n\n%s · %s", public.Message, public.Code, id)
	fmt.Fprintln(os.Stderr, message)
	if err := showApplicationFailure(message); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
