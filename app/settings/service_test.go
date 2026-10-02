package settings

import (
	"os"
	"testing"

	"cczjVideo/app/db"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-settings-test-")
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

// 这个门面只有两个方法，值又直接决定更新流程（ignored_version、last_start_version）
// 和数据新鲜度。接线接反时读写会各自成功、互相对不上，所以只验一条：写进去的读得回来。
func TestSetThenGetRoundTrips(t *testing.T) {
	svc := NewService()
	const key = "media_settings_probe"

	value, err := svc.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if value != "" {
		t.Fatalf("新键的初值 = %q, 期望空", value)
	}

	if err := svc.Set(key, "2.2.1"); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Get(key); err != nil || got != "2.2.1" {
		t.Fatalf("回读 = %q, err = %v, 期望 2.2.1", got, err)
	}

	if err := svc.Set(key, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Get(key); err != nil || got != "" {
		t.Fatalf("清空后回读 = %q, err = %v, 期望空", got, err)
	}
}
