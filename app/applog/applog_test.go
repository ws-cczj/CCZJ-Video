package applog

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFormatFieldsOrdersKeys(t *testing.T) {
	got := formatFields(Fields{"task_id": "task-1", "source_key": "demo"})
	want := ` fields{source_key="demo",task_id="task-1"}`
	if got != want {
		t.Fatalf("formatFields() = %q, want %q", got, want)
	}
}

// tempLogger 把包级单例改指向临时目录。
// Init 被 sync.Once 保护，而 Default() 在单例为空时会拿生产目录把它建出来——
// 测试里只要先碰到 Default()，日志就直接落进用户真实的 %APPDATA% 日志文件了。
func tempLogger(t *testing.T) *Logger {
	t.Helper()
	l := &Logger{logDir: t.TempDir(), keepDays: 30}
	if err := l.rotateIfNeeded(); err != nil {
		t.Fatalf("rotateIfNeeded: %v", err)
	}
	once = sync.Once{}
	defaultLogger = l
	// 句柄不关掉 Windows 下删不掉 TempDir，所以 Cleanup 里必须 Close
	t.Cleanup(func() {
		l.Close()
		once = sync.Once{}
		defaultLogger = nil
	})
	return l
}

func TestCallerPointsAtLogSite(t *testing.T) {
	l := tempLogger(t)
	since := l.LatestSeq()
	_, file, line, _ := runtime.Caller(0)
	Info("caller probe")

	var got string
	for _, rec := range l.Since(since, 10) {
		if rec.Message == "caller probe" {
			got = rec.Caller
		}
	}
	want := filepath.Base(file) + ":" + strconv.Itoa(line+1)
	if got != want {
		t.Fatalf("caller = %q, want the log statement itself %q", got, want)
	}
}

func emitShallow(msg string) { InfoAt(1, "%s", msg) }

func emitDeepInner(extra int, msg string) { InfoAt(extra, "%s", msg) }

// emitDeep 复刻 Engine.logAt 的形状：业务点 -> emitDeep -> emitDeepInner -> InfoAt
func emitDeep(msg string) { emitDeepInner(2, msg) }

func TestCallerAtSkipsWrapperFrames(t *testing.T) {
	l := tempLogger(t)

	check := func(msg string, emit func(string)) {
		t.Helper()
		since := l.LatestSeq()
		_, file, line, _ := runtime.Caller(0)
		emit(msg)

		var got string
		for _, rec := range l.Since(since, 10) {
			if rec.Message == msg {
				got = rec.Caller
			}
		}
		want := filepath.Base(file) + ":" + strconv.Itoa(line+1)
		if got != want {
			t.Fatalf("caller = %q, want the business call site %q", got, want)
		}
	}

	check("shallow wrapper", emitShallow)
	check("deep wrapper", emitDeep)
}

// TestParseRecordLineRoundTripsWrittenLines 用 Logger 真实写出的行做往返断言。
// 写盘和读回是两条独立代码路径，历史文件浏览完全依赖 ParseRecordLine 复原，
// 只要两边格式一错位，设置页的「历史文件」就会永远显示没有匹配。
func TestParseRecordLineRoundTripsWrittenLines(t *testing.T) {
	l := tempLogger(t)
	Info("启动完成")
	Warn("反爬静默中")

	path := filepath.Join(l.Dir(), "cczj-"+time.Now().Format("2006-01-02")+".log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取日志文件: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("写出 %d 行，want 2", len(lines))
	}
	wantLevel := []string{"INFO", "WARN"}
	wantMsg := []string{"启动完成", "反爬静默中"}
	for i, lineText := range lines {
		rec, ok := ParseRecordLine(strings.TrimPrefix(lineText, "\uFEFF"))
		if !ok {
			t.Fatalf("第 %d 行 %q 解析失败", i, lineText)
		}
		if rec.Level != wantLevel[i] {
			t.Errorf("第 %d 行 level = %q, want %q", i, rec.Level, wantLevel[i])
		}
		if rec.Message != wantMsg[i] {
			t.Errorf("第 %d 行 message = %q, want %q", i, rec.Message, wantMsg[i])
		}
		if rec.Caller == "" {
			t.Errorf("第 %d 行 caller 为空", i)
		}
	}
}
