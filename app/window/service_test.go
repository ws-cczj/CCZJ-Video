package window

import (
	"os"
	"strconv"
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/settings"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-window-test-")
	if err != nil {
		panic(err)
	}
	if err := db.InitDB(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	db.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func newSvc(t *testing.T) *Service {
	t.Helper()
	// current 传 nil 就等于「窗口还不存在」：所有方法都得活着回来。
	return NewService(nil, settings.NewService())
}

func stored(t *testing.T, key string) string {
	t.Helper()
	value, err := settings.NewService().Get(key)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// 出厂默认是「关闭按钮缩到托盘」。库里读不到值时返回的是 ("", nil)，
// 把它当成「用户存过 false」就会让每个新装用户第一次点关闭直接退出程序。
func TestLoadCloseBehaviorPreservesTheFirstRunDefault(t *testing.T) {
	svc := newSvc(t)
	if _, err := db.DB().Exec(`DELETE FROM settings WHERE key='close_to_tray'`); err != nil {
		t.Fatal(err)
	}

	if !svc.CloseBehavior() {
		t.Fatal("构造后的默认行为就应是缩到托盘")
	}
	svc.LoadCloseBehavior()
	if !svc.CloseBehavior() {
		t.Fatal("没存过设置时，LoadCloseBehavior 不许改掉默认值")
	}
}

func TestCloseBehaviorRoundTripsBothWays(t *testing.T) {
	svc := newSvc(t)

	svc.SetCloseBehavior(false)
	if svc.CloseBehavior() {
		t.Fatal("SetCloseBehavior(false) 之后应立即生效")
	}
	reloaded := newSvc(t)
	reloaded.LoadCloseBehavior()
	if reloaded.CloseBehavior() {
		t.Fatal("重开应用后应恢复成「直接退出」")
	}

	svc.SetCloseBehavior(true)
	if stored(t, "close_to_tray") != "1" {
		t.Fatalf("持久化的值 = %q, 期望 \"1\"", stored(t, "close_to_tray"))
	}
	reloaded = newSvc(t)
	reloaded.LoadCloseBehavior()
	if !reloaded.CloseBehavior() {
		t.Fatal("重开应用后应恢复成「缩到托盘」")
	}
}

// 尺寸必须夹到下限再落盘：存进去 0，下次启动恢复出的就是一个看不见的窗口。
func TestSetSizeClampsBeforePersisting(t *testing.T) {
	svc := newSvc(t)
	svc.SetSize(640, 360)

	if got := stored(t, "window_width"); got != strconv.Itoa(minimumWidth) {
		t.Fatalf("落盘宽度 = %q, 期望下限 %d", got, minimumWidth)
	}
	if got := stored(t, "window_height"); got != strconv.Itoa(minimumHeight) {
		t.Fatalf("落盘高度 = %q, 期望下限 %d", got, minimumHeight)
	}

	svc.SetSize(1920, 1080)
	if got := stored(t, "window_width"); got != "1920" {
		t.Fatalf("大于下限的宽度应原样落盘, 得到 %q", got)
	}
}

// 窗口还没有的窗口期里调用这些方法不能崩：启动顺序上确实存在这一刻。
func TestWindowMethodsAreSafeBeforeAWindowExists(t *testing.T) {
	svc := newSvc(t)

	if svc.Size() != (Size{}) {
		t.Fatal("没有窗口时尺寸应是零值")
	}
	if svc.IsMax() {
		t.Fatal("没有窗口时不该报告已最大化")
	}
	if svc.IsFullscreen() {
		t.Fatal("没有窗口时不该报告全屏")
	}
	if svc.ToggleMax() {
		t.Fatal("没有窗口时切换最大化应返回 false")
	}
	svc.SetFullscreen(true)
	svc.SetResizable(false)
	svc.ReloadTitleBar()
	svc.ApplySettings()
	if svc.Resizable() {
		t.Fatal("没有窗口时不该报告可调整大小")
	}
}
