//go:build !windows

package handoff

// 热替换与交接只存在于 Windows 这一条路（Await 第一行就用 runtime.GOOS 挡住了），
// 这两问在非 Windows 上恒为"没人在跑"，存在只为让包在任何平台都编得过。
func processAlive(int) bool { return false }

func sameImagePIDs(string) []int { return nil }
