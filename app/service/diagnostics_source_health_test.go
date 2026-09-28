package service

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

// 阈值判定住在服务层：连败次数是 db 取出来的，"几次算失效"是这里定的。
// 巡检分支排在采集分支之前，因为巡检探的是单页能否应答，比整轮采集更早发现地址已经废了。
func TestDiagnosticsSourcesWarnsDeadSourceOnlyAtStreakThreshold(t *testing.T) {
	dir, err := os.MkdirTemp("", "cczj-service-diag-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitDB(dir); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	// SQLite 句柄不放开，Windows 上就删不掉临时目录；先按源键收干净自己写下的行，
	// 免得 sync.Once 让下一轮 run 撞上 sources.source_key 的 UNIQUE。
	t.Cleanup(func() {
		_, _ = db.DB().Exec(`DELETE FROM sources WHERE source_key LIKE 'diag_streak%'`)
		_, _ = db.DB().Exec(`DELETE FROM source_health WHERE source_key LIKE 'diag_streak%'`)
		db.Close()
		_ = os.RemoveAll(dir)
	})

	addStreakSource(t, "diag_streak_patrol", "巡检失效源")
	addStreakSource(t, "diag_streak_collect", "采集失效源")
	addStreakSource(t, "diag_streak_shaky", "抖动源")

	base := time.Unix(1700000000, 0)
	recordFails := func(sourceKey, kind string, times int) {
		t.Helper()
		for i := 1; i <= times; i++ {
			if err := db.RecordSourceHealth(sourceKey, kind, false, 10, 0, fmt.Sprintf("第 %d 次失败", i), base.Add(time.Duration(i)*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
	recordFails("diag_streak_patrol", db.SourceHealthKindPatrol, sourceDeadStreak)
	recordFails("diag_streak_collect", db.SourceHealthKindCollect, sourceDeadStreak)
	// 少一次就是抖动量级：阈值这条线只能踩到，不能越过半步。
	recordFails("diag_streak_shaky", db.SourceHealthKindPatrol, sourceDeadStreak-1)

	var notes []string
	rows := (&App{}).diagnosticsSources(func(format string, args ...any) {
		notes = append(notes, fmt.Sprintf(format, args...))
	})

	byKey := make(map[string]DiagSourceRow, len(rows))
	for _, row := range rows {
		byKey[row.SourceKey] = row
	}
	if got := byKey["diag_streak_patrol"].PatrolHealth.FailStreak; got != sourceDeadStreak {
		t.Fatalf("巡检连败 = %d, want %d（服务层判定的输入取自这里）", got, sourceDeadStreak)
	}

	var deadNotes []string
	for _, n := range notes {
		if strings.Contains(n, "可能已失效") {
			deadNotes = append(deadNotes, n)
		}
	}
	if len(deadNotes) != 2 {
		t.Fatalf("连败到阈值的源只有两个，失效提示应有 2 条，got %d: %v", len(deadNotes), notes)
	}
	joined := strings.Join(deadNotes, "\n")
	if !strings.Contains(joined, "巡检失效源") || !strings.Contains(joined, "采集失效源") {
		t.Fatalf("巡检与采集两条分支都该点名各自的源:\n%s", joined)
	}
	if strings.Contains(joined, "抖动源") {
		t.Fatalf("连败差一次就该是抖动，不该被判成失效:\n%s", joined)
	}
}

func addStreakSource(t *testing.T, key, name string) {
	t.Helper()
	if err := db.AddSource(&model.Source{
		SourceKey: key,
		Name:      name,
		ApiUrl:    "https://example.test/api.php",
		Enabled:   1,
	}); err != nil {
		t.Fatalf("add source %s: %v", key, err)
	}
}
