package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	downloadservice "cczjVideo/app/download"
)

// 断点续传的进度写在 %APPDATA%\CCZJ Video\downloads.json。新版多写了 segment_details，
// 旧版本写的文件必须照样能读出来 —— 读不出来等于用户的下载队列凭空消失。
func TestLegacyDownloadsFileRestoresAsPlainSegments(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	dir := filepath.Join(root, "CCZJ Video")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const first = "https://cdn.example.test/1.ts"
	const second = "https://cdn.example.test/2.ts"
	legacy := `[{"task_id":"old","url":"https://cdn.example.test/a.m3u8","filename":"a.ts",` +
		`"save_path":"` + filepath.ToSlash(filepath.Join(dir, "a.ts")) + `","total":20,"downloaded":10,` +
		`"status":"downloading","seg_index":1,"is_m3u8":true,"segments":["` + first + `","` + second + `"]}]`
	if err := os.WriteFile(filepath.Join(dir, "downloads.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{downloads: downloadservice.NewRegistry[downloadTask]()}
	app.loadPersistedTasks()
	task, ok := app.downloads.Get("old")
	if !ok {
		t.Fatal("legacy task was not restored")
	}
	if len(task.playlist) != 0 {
		t.Fatalf("playlist = %#v, want empty for a file written without segment_details", task.playlist)
	}
	if got := task.segments; !reflect.DeepEqual(got, []string{first, second}) {
		t.Fatalf("segments = %v, want %v", got, []string{first, second})
	}

	// 续传时按明文整片处理：这是旧版本的行为，绝不能凭空造出一把密钥。
	segments, err := app.resolvePlaylist(context.Background(), task, "https://cdn.example.test/a.m3u8", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(segments))
	}
	if segments[0].URL != first || segments[0].Key != nil || segments[0].Range != nil {
		t.Fatalf("restored segment = %#v", segments[0])
	}
	if task.nextSegIdx() != 1 {
		t.Fatalf("resume index = %d, want 1 (断点必须留在原处)", task.nextSegIdx())
	}
}

func TestPersistedDownloadsKeepSegmentDetailsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	dir := filepath.Join(root, "CCZJ Video")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	app := &App{downloads: downloadservice.NewRegistry[downloadTask]()}
	key := &downloadservice.Key{Method: "AES-128", URI: "https://cdn.example.test/k"}
	rng := &downloadservice.ByteRange{Length: 100, Start: 200}
	task := &downloadTask{
		status:   VideoDownloadStatus{TaskId: "new", SavePath: filepath.Join(dir, "b.ts"), Status: "paused", Url: "https://cdn.example.test/b.m3u8"},
		isM3u8:   true,
		segIndex: 1,
		playlist: []downloadservice.Segment{{URL: "https://cdn.example.test/b.m3u8", Seq: 0}, {URL: "https://cdn.example.test/big.mp4", Seq: 1, Key: key, Range: rng}},
		segments: []string{"https://cdn.example.test/b.m3u8", "https://cdn.example.test/big.mp4"},
	}
	app.downloads.Add("new", task)
	app.savePersistedTasks()

	reloaded := &App{downloads: downloadservice.NewRegistry[downloadTask]()}
	reloaded.loadPersistedTasks()
	restored, ok := reloaded.downloads.Get("new")
	if !ok {
		t.Fatal("task was not restored")
	}
	if !reflect.DeepEqual(restored.playlist, task.playlist) {
		t.Fatalf("playlist = %#v, want %#v", restored.playlist, task.playlist)
	}

	// 内存里已有结构化列表时不必再抓播放列表：列表地址的签名往往已经过期。
	segments, err := reloaded.resolvePlaylist(context.Background(), restored, "https://cdn.example.test/b.m3u8", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[1].Key == nil || segments[1].Range == nil {
		t.Fatalf("segments = %#v", segments)
	}
}
