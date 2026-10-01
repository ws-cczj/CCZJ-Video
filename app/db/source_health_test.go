package db

import (
	"fmt"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSourceHealthRecordsPrunesAndSummarizesPerKind(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() { instance, dataDir = prevInstance, prevDir })

	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	const sourceKey = "health_test"
	base := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	empty, err := GetSourceHealth(sourceKey, SourceHealthKindCollect, 20)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Samples != 0 || empty.LastOK {
		t.Fatalf("没有样本的源必须读成\"没观测过\"，got %+v", empty)
	}

	// 采集：两次成功一次失败，且失败是最后一条。
	for i, ok := range []bool{true, true, false} {
		errText := ""
		if !ok {
			errText = "第 3 页取页失败"
		}
		if err := RecordSourceHealth(sourceKey, SourceHealthKindCollect, ok, int64(1500+i), 20+i, errText, base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	// 巡检：一次成功，延迟与采集样本不同量级，两类不能互相污染。
	if err := RecordSourceHealth(sourceKey, SourceHealthKindPatrol, true, 42, 0, "", base.Add(5*time.Hour)); err != nil {
		t.Fatal(err)
	}

	collect, err := GetSourceHealth(sourceKey, SourceHealthKindCollect, 20)
	if err != nil {
		t.Fatal(err)
	}
	if collect.Samples != 3 || collect.OKs != 2 {
		t.Fatalf("采集汇总 = %+v", collect)
	}
	if collect.SuccessRate != 67 {
		t.Errorf("成功率 = %d, want 67", collect.SuccessRate)
	}
	if collect.AvgLatencyMS < 1500 || collect.AvgLatencyMS > 1501 {
		t.Errorf("平均延迟被巡检样本污染: %d", collect.AvgLatencyMS)
	}
	if collect.LastOK {
		t.Error("最后一条采集样本是失败，LastOK 不能为真")
	}
	if collect.LastError != "第 3 页取页失败" {
		t.Errorf("LastError = %q", collect.LastError)
	}
	if collect.FailStreak != 1 {
		t.Errorf("FailStreak = %d, want 1", collect.FailStreak)
	}
	if collect.LastSampleUnix != base.Add(2*time.Hour).Unix() {
		t.Errorf("LastSampleUnix = %d", collect.LastSampleUnix)
	}

	patrol, err := GetSourceHealth(sourceKey, SourceHealthKindPatrol, 20)
	if err != nil {
		t.Fatal(err)
	}
	if patrol.Samples != 1 || patrol.OKs != 1 || patrol.FailStreak != 0 {
		t.Fatalf("巡检汇总 = %+v", patrol)
	}
	if patrol.AvgLatencyMS != 42 {
		t.Errorf("巡检平均延迟 = %d, want 42", patrol.AvgLatencyMS)
	}

	// 窗口只截最近 N 条：只看 2 条时最早那次成功不在统计里，成功率随之变化。
	recent, err := GetSourceHealth(sourceKey, SourceHealthKindCollect, 2)
	if err != nil {
		t.Fatal(err)
	}
	if recent.Samples != 2 || recent.SuccessRate != 50 {
		t.Fatalf("窗口 2 条的汇总 = %+v", recent)
	}

	rows, err := ListSourceHealth(sourceKey, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("两类掺在一起应有 4 条，got %d", len(rows))
	}
	if rows[0].TsUnix < rows[1].TsUnix {
		t.Error("历史列表必须新→旧")
	}
	collectOnly, err := ListSourceHealth(sourceKey, SourceHealthKindCollect, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(collectOnly) != 3 {
		t.Fatalf("按 kind 过滤后应有 3 条，got %d", len(collectOnly))
	}

	if err := ResetSourceHealth(DB(), sourceKey); err != nil {
		t.Fatal(err)
	}
	after, err := GetSourceHealth(sourceKey, SourceHealthKindCollect, 20)
	if err != nil {
		t.Fatal(err)
	}
	if after.Samples != 0 {
		t.Fatalf("重置后仍有样本: %+v", after)
	}
}

func TestSourceHealthPruneKeepsOnlyNewestPerSource(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() { instance, dataDir = prevInstance, prevDir })

	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	const sourceKey = "health_prune"
	base := time.Unix(1700000000, 0)
	// 越过保留上限，验证淘汰的是最旧的而不是随机行。
	for i := 0; i < sourceHealthKeep+30; i++ {
		if err := RecordSourceHealth(sourceKey, SourceHealthKindCollect, true, int64(i), 1, "", base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := instance.Get(&count, `SELECT COUNT(*) FROM source_health WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if count != sourceHealthKeep {
		t.Fatalf("样本数 = %d, want %d", count, sourceHealthKeep)
	}
	rows, err := ListSourceHealth(sourceKey, SourceHealthKindCollect, sourceHealthKeep)
	if err != nil {
		t.Fatal(err)
	}
	if rows[len(rows)-1].LatencyMS != int64(30) {
		t.Fatalf("保留的最旧一条应是第 31 条（latency=30），got %d", rows[len(rows)-1].LatencyMS)
	}

	// 淘汰按源各自算：另一个源的样本不能被牵连删掉。
	other := "health_other"
	for i := 0; i < 3; i++ {
		if err := RecordSourceHealth(other, SourceHealthKindPatrol, false, 1, 0, fmt.Sprintf("e%d", i), base); err != nil {
			t.Fatal(err)
		}
	}
	otherRows, err := ListSourceHealth(other, "", sourceHealthKeep)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherRows) != 3 {
		t.Fatalf("另一个源的样本被误删，剩 %d 条", len(otherRows))
	}
}

// 错误文本原样进库、原样渲染，长起来没有上限：必须按字节截，且切点要落在
// rune 边界上，否则半个汉字在诊断页上就是乱码。
func TestSourceHealthTrimsLongErrorWithoutSplittingRune(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() { instance, dataDir = prevInstance, prevDir })

	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	long := "E"
	for i := 0; i < 200; i++ {
		long += "错误"
	}
	if err := RecordSourceHealth("health_trim", SourceHealthKindCollect, false, 1, 0, long, time.Unix(1700000000, 0)); err != nil {
		t.Fatal(err)
	}
	rows, err := ListSourceHealth("health_trim", SourceHealthKindCollect, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	// 首字节是 ASCII，于是 300 这个切点正好落在某个汉字的中间——截断必须往回退，
	// 而不是把半个字留在串尾。
	if len(rows[0].Err) > sourceHealthErrBytes {
		t.Fatalf("错误文本未截断，len=%d", len(rows[0].Err))
	}
	if len(rows[0].Err) < sourceHealthErrBytes-3 {
		t.Fatalf("截得太狠，len=%d", len(rows[0].Err))
	}
	if !utf8.ValidString(rows[0].Err) {
		t.Fatalf("截断切坏了 rune: %q", rows[0].Err[len(rows[0].Err)-3:])
	}
}

// 连败口径必须钉死：它是"抖动"与"已下线"之间唯一的读数，服务层拿它对着阈值判失效。
// 累计要从最后一次成功之后重算，中途任何一次成功都得清零——否则源站只要红过一次，
// 连败数就永远挂在高位，之后每次抖动都会被读成"已失效"。
func TestSourceHealthFailStreakAccumulatesAndClearsOnSuccess(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() { instance, dataDir = prevInstance, prevDir })

	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	const sourceKey = "health_streak"
	base := time.Unix(1700000000, 0)

	patrolStreak := func() int {
		t.Helper()
		health, err := GetSourceHealth(sourceKey, SourceHealthKindPatrol, 20)
		if err != nil {
			t.Fatal(err)
		}
		return health.FailStreak
	}
	record := func(kind string, ok bool, at time.Time, errText string) {
		t.Helper()
		if err := RecordSourceHealth(sourceKey, kind, ok, 10, 0, errText, at); err != nil {
			t.Fatal(err)
		}
	}

	// 还没有过一次成功，于是全部失败都算连败，一路累计到服务层的阈值量级。
	for i := 1; i <= 3; i++ {
		record(SourceHealthKindPatrol, false, base.Add(time.Duration(i)*time.Minute), fmt.Sprintf("第 %d 次巡检失败", i))
		if got := patrolStreak(); got != i {
			t.Fatalf("第 %d 次失败后的连败 = %d, want %d", i, got, i)
		}
	}
	if got := patrolStreak(); got < 3 {
		t.Fatalf("连败没能累计到阈值量级: %d", got)
	}

	// 一次成功把连败清零。
	record(SourceHealthKindPatrol, true, base.Add(10*time.Minute), "")
	if got := patrolStreak(); got != 0 {
		t.Fatalf("一次成功后的连败 = %d, want 0", got)
	}

	// 清零后从头起算，而不是接着历史往上加。
	record(SourceHealthKindPatrol, false, base.Add(20*time.Minute), "又失败一次")
	if got := patrolStreak(); got != 1 {
		t.Fatalf("成功之后再次失败的连败 = %d, want 1", got)
	}

	// 连败按 kind 各算各的：采集侧的成功清不掉巡检侧的连败，两类样本本就不同源。
	record(SourceHealthKindCollect, true, base.Add(30*time.Minute), "")
	if got := patrolStreak(); got != 1 {
		t.Fatalf("另一类的成功不该清零，got %d", got)
	}
}

// 连败边界的第二个读法：同一秒里的样本要靠 id 分先后。
//
// 这条不是学术情形——自动停用现在直接读这个数，而测试、脚本导入、同一轮里
// 连续几条样本都会挤在同一秒。只比 ts 会把"早于成功的失败"也算进连败，
// 于是刚恢复的源被立刻再停用。
func TestFailStreakOrdersSameSecondSamplesByRowID(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() { instance, dataDir = prevInstance, prevDir })
	if err := createTables(); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(); err != nil {
		t.Fatal(err)
	}

	const sourceKey = "same_second_streak"
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, ok := range []bool{false, false, true, false} {
		if err := RecordSourceHealth(sourceKey, SourceHealthKindCollect, ok, 10, 0, "取页失败", at); err != nil {
			t.Fatal(err)
		}
	}

	health, err := GetSourceHealth(sourceKey, SourceHealthKindCollect, 0)
	if err != nil {
		t.Fatal(err)
	}
	if health.FailStreak != 1 {
		t.Fatalf("同一秒内的连败 = %d, want 1（成功之前的两条失败不能算进来）", health.FailStreak)
	}
	if health.LastOK {
		t.Fatal("最后一条是失败，LastOK 不该为真")
	}
}
