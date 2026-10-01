package db

import (
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

// 身份判定只有 identity.go 一份实现，这几条测试钉住它的三个关键性质：
// 归一化认得全角与零宽字符、阶梯不会跨季/跨类型乱认领、迁移合并历史重复行
// 时不丢收藏与观看进度。

func TestNormalizeTitleDropsWhitespaceAndFullWidthPunctuation(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"阿凡达：水之道", "阿凡达:水之道"},
		{"老友记  第六季！", "老友记第六季!"},
		{"三体， 问题", "三体,问题"},
		{"\u200b零宽\u00a0空格 ", "零宽空格"},
		{"\u3000全角空格", "全角空格"},
		{"Avatar： The Way of Water", "avatar:thewayofwater"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeTitle(c.input); got != c.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", c.input, got, c.want)
		}
		// 归一化必须幂等：迁移回填和写入路径各算一次，结果不能不同。
		if twice := normalizeTitle(normalizeTitle(c.input)); twice != c.want {
			t.Errorf("normalizeTitle not idempotent for %q: %q", c.input, twice)
		}
	}
	if titleAliasKey("老友记 第六季！") != titleAliasKey("老友记第六季") {
		t.Error("titleAliasKey should ignore punctuation differences")
	}
	if titleAliasKey("权力的游戏第三季") == titleAliasKey("权力的游戏第八季") {
		t.Error("titleAliasKey must keep season numbers distinct")
	}
}

// 源站把清晰度/封装格式拼在标题末尾，留进 name_norm 就是给同一部片另立身份。
// 剥尾巴有两道边界：不能啃掉拉丁单词的尾巴（Palermo 的 rm），也不能把整串剥成空
// （真有一部片叫 Cam）；语言与字幕标记属于另一回事，剥掉会把用户当两部收的并成一部。
func TestStripQualityNoiseOnlyRemovesTrailingFormatTags(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"雷神4：爱与雷霆_1080P_", "雷神4：爱与雷霆"},
		{"某某某 1080P", "某某某"},
		{"某某某_HD", "某某某"},
		{"疯狂原始人2_蓝光", "疯狂原始人2"},
		{"某片_1080P_x264", "某片"},
		{"冰与火之歌 4K", "冰与火之歌"},
		{"大侦探波洛.avi", "大侦探波洛"},
		// 整串就是个画质词：留着原样，剥成空串等于这条记录没有身份。
		{"Cam", "Cam"},
		{"1080P", "1080P"},
		// 不是画质尾的写法一律不动。
		{"Palermo", "Palermo"},
		{"速度与激情5", "速度与激情5"},
		{"老友记 粤语", "老友记 粤语"},
		{"某片 中文字幕", "某片 中文字幕"},
		{"", ""},
	}
	for _, c := range cases {
		if got := stripQualityNoise(c.input); got != c.want {
			t.Errorf("stripQualityNoise(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// 画质尾巴与干净标题必须归一到同一个键，否则唯一索引拦不住第二次入库。
func TestNormalizeTitleCollapsesQualityTailWithCleanTitle(t *testing.T) {
	pairs := [][2]string{
		{"雷神4：爱与雷霆_1080P_", "雷神4：爱与雷霆"},
		{"疯狂原始人2 蓝光", "疯狂原始人2"},
		{" Palermo ", "Palermo"},
	}
	for _, pair := range pairs {
		if normalizeTitle(pair[0]) != normalizeTitle(pair[1]) {
			t.Errorf("%q 与 %q 归一后不同: %q vs %q",
				pair[0], pair[1], normalizeTitle(pair[0]), normalizeTitle(pair[1]))
		}
	}
	// 反过来：画质词不同的两个发行版本不该被这条规则误并。
	if normalizeTitle("某片 粤语") == normalizeTitle("某片") {
		t.Error("语言标记不该被当成画质尾巴剥掉")
	}
}

func TestGlobalVideoIndexMatchLadderTiers(t *testing.T) {
	index := newGlobalVideoIndex([]globalCandidate{
		{ID: 1, VodName: "阿凡达：水之道", TypeID: 3, Year: "2022"},
		{ID: 2, VodName: "三体", TypeID: 3, Year: "2023"},
		{ID: 3, VodName: "花木兰", TypeID: 7, Year: "1998"},
		{ID: 4, VodName: "无名占位", TypeID: 0, Year: ""},
	})

	cases := []struct {
		name   string
		title  string
		year   string
		typeID int64
		wantID int64
		want   matchTier
	}{
		{"exact", "阿凡达：水之道", "", 3, 1, tierExact},
		{"norm", "阿凡达:水之道", "", 3, 1, tierNorm},
		{"alias", "三体！", "", 3, 2, tierAlias},
		{"unknown type claims any row", "花木兰", "", 0, 3, tierExact},
		{"placeholder row is claimable", "无名占位", "", 9, 4, tierExact},
		{"same title other type stays separate", "花木兰", "", 8, 0, tierNew},
		{"season suffix never folds into the base title", "花木兰 第二季", "", 7, 0, tierNew},
		{"unrelated title", "不存在的一片", "", 3, 0, tierNew},
	}
	for _, c := range cases {
		id, tier := index.match(c.title, c.year, c.typeID)
		if id != c.wantID || tier != c.want {
			t.Errorf("%s: match(%q, type=%d) = (%d, %s), want (%d, %s)", c.name, c.title, c.typeID, id, tier, c.wantID, c.want)
		}
	}

	// 判断看的是"两个名字里谁多了个季号"，与传入顺序无关；阶梯两个方向都要拒绝。
	if !hasSeasonSuffix("权力的游戏 第二季", "权力的游戏") {
		t.Error("hasSeasonSuffix must flag a season suffix added to the base title")
	}
	if !hasSeasonSuffix("权力的游戏", "权力的游戏 第二季") {
		t.Error("hasSeasonSuffix must be symmetric in its arguments")
	}
	if hasSeasonSuffix("权力的游戏 第二季", "权力的游戏 第二季") {
		t.Error("identical titles must not be flagged")
	}
}

// add 与 update 决定同一批次里后续记录能否命中刚建/刚补的行。
func TestGlobalVideoIndexAddAndUpdateWithinBatch(t *testing.T) {
	index := newGlobalVideoIndex(nil)
	if _, tier := index.match("长相思", "", 5); tier != tierNew {
		t.Fatalf("empty index should report new, got %s", tier)
	}
	index.add(globalCandidate{ID: 42, VodName: "长相思", TypeID: 5, Year: "2023"})
	id, tier := index.match("长相思", "2023", 5)
	if id != 42 || tier != tierExact {
		t.Fatalf("after add: got (%d, %s), want (42, exact)", id, tier)
	}

	// 类型补齐前是占位行，补齐后同批次另一条同类型记录必须能认领它。
	index.update(42, 6, "2024")
	id, tier = index.match("长相思", "", 6)
	if id != 42 || tier != tierExact {
		t.Fatalf("after update: got (%d, %s), want (42, exact)", id, tier)
	}
}

// 迁移合并历史重复行是 A4 唯一有损风险的写操作：收藏、观看进度、目录引用
// 都必须落到存活行上，进度取更大的一侧。
func TestMigrateDedupeGlobalVideoMergesDuplicatesAndKeepsReferences(t *testing.T) {
	dir := t.TempDir()
	database, err := sqlx.Connect("sqlite", filepath.Join(dir, "cczj_video.db"))
	if err != nil {
		t.Fatal(err)
	}
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	defer func() {
		instance, dataDir = prevInstance, prevDir
		_ = database.Close()
	}()

	if err := createTables(); err != nil {
		t.Fatal(err)
	}
	// 造出漂移的两条身份：一条全角冒号且字段空，一条半角且带着豆瓣分数与海报。
	// name_norm 都留空，模拟归一化列还没落地时的用户库。
	const emptyRowID, richRowID = 7001, 7002
	if _, err := database.Exec(`DELETE FROM global_video WHERE id IN (?, ?)`, emptyRowID, richRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO global_video (id, vod_name, type_id, year, douban_score, pic)
		VALUES (?, '我的团长：我的团', 3, '2008', '', ''), (?, '我的团长:我的团', 3, '2008', '9.4', 'http://pic')`,
		emptyRowID, richRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO favorites (global_id, source_key, vod_id)
		VALUES (?, 'src-a', 'vod-1'), (?, 'src-b', 'vod-2')`, emptyRowID, richRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO watch_history (global_id, source_key, vod_id, ep_num, position)
		VALUES (?, 'src-a', 'vod-1', 1, 12), (?, 'src-a', 'vod-1', 1, 88), (?, 'src-b', 'vod-2', 2, 5)`,
		emptyRowID, richRowID, richRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name)
		VALUES ('src-b', 'vod-2', ?, '我的团长:我的团')`, richRowID); err != nil {
		t.Fatal(err)
	}
	// 停在 v1：模拟一个已经跑过上一版迁移、但还没做归一化的用户库。
	if _, err := database.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	var remaining []int64
	if err := database.Select(&remaining, `SELECT id FROM global_video WHERE id IN (?, ?) ORDER BY id`, emptyRowID, richRowID); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("duplicate rows after merge = %v, want exactly one", remaining)
	}
	survivor := remaining[0]
	if survivor != richRowID {
		t.Fatalf("survivor = %d, want the row already carrying douban_score/pic (%d)", survivor, richRowID)
	}

	var score, pic string
	if err := database.Get(&score, `SELECT douban_score FROM global_video WHERE id=?`, survivor); err != nil {
		t.Fatal(err)
	}
	if err := database.Get(&pic, `SELECT pic FROM global_video WHERE id=?`, survivor); err != nil {
		t.Fatal(err)
	}
	if score != "9.4" || pic != "http://pic" {
		t.Fatalf("merged fields lost: score=%q pic=%q", score, pic)
	}

	var favKeys []string
	if err := database.Select(&favKeys, `SELECT source_key||'/'||vod_id FROM favorites WHERE global_id=? ORDER BY 1`, survivor); err != nil {
		t.Fatal(err)
	}
	if len(favKeys) != 2 {
		t.Fatalf("favorites after merge = %v, want both sources kept", favKeys)
	}

	var position float64
	if err := database.Get(&position, `SELECT position FROM watch_history WHERE global_id=? AND ep_num=1`, survivor); err != nil {
		t.Fatal(err)
	}
	if position != 88 {
		t.Fatalf("watch progress = %v, want the larger one (88)", position)
	}
	var ep2 int
	if err := database.Get(&ep2, `SELECT COUNT(*) FROM watch_history WHERE global_id=? AND ep_num=2`, survivor); err != nil {
		t.Fatal(err)
	}
	if ep2 != 1 {
		t.Fatalf("second episode history rows = %d, want 1", ep2)
	}

	var catalogRefs int
	if err := database.Get(&catalogRefs, `SELECT COUNT(*) FROM source_videos WHERE global_id=?`, survivor); err != nil {
		t.Fatal(err)
	}
	if catalogRefs != 1 {
		t.Fatalf("source_videos pointing at survivor = %d, want 1", catalogRefs)
	}

	// 合并后 (name_norm, type_id) 唯一索引必须真的挡住同名重复插入。
	var indexCount int
	if err := database.Get(&indexCount,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_gv_name_type_norm'`); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatal("unique normalized index was not rebuilt after the merge")
	}
	if _, err := database.Exec(`INSERT INTO global_video (vod_name, name_norm, type_id)
		VALUES ('我的团长：我的团', '我的团长:我的团', 3)`); err == nil {
		t.Fatal("duplicate identity insert must be rejected by the unique index")
	}
}

// 空标题的行没有可比身份：既不该被合并掉，也不该互相撞唯一索引。
func TestPartialUniqueIndexLeavesEmptyTitlesAlone(t *testing.T) {
	dir := t.TempDir()
	database, err := sqlx.Connect("sqlite", filepath.Join(dir, "cczj_video.db"))
	if err != nil {
		t.Fatal(err)
	}
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	defer func() {
		instance, dataDir = prevInstance, prevDir
		_ = database.Close()
	}()

	if err := createTables(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO global_video (vod_name, type_id) VALUES ('', 0), ('', 0)`); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	var empty int
	if err := database.Get(&empty, `SELECT COUNT(*) FROM global_video WHERE vod_name=''`); err != nil {
		t.Fatal(err)
	}
	if empty != 2 {
		t.Fatalf("empty-title rows = %d, want both untouched", empty)
	}
}

// insertGlobalVideo 必须把 Go 侧算出的 name_norm 一起写进去：列留空就等于
// 这条身份对唯一索引不可见，下一批同名记录会再插一条。
func TestInsertGlobalVideoWritesNameNorm(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const title = "归一化写入校验：测试"
	if _, err := DB().Exec(`DELETE FROM global_video WHERE vod_name=?`, title); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = DB().Exec(`DELETE FROM global_video WHERE vod_name=?`, title) }()

	id, err := insertGlobalVideo(DB(), title, 11)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := DB().Get(&stored, `SELECT name_norm FROM global_video WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if want := normalizeTitle(title); stored != want {
		t.Fatalf("name_norm = %q, want %q", stored, want)
	}
}

// 单条调用方（备份导入、详情页补录）不共享采集期那份索引，改为先试 vod_name/name_norm
// 两个索引直接答得出来的两档。快路径必须和共享索引的阶梯同解，否则导入会把已经在库里的
// 片子另起一条身份。
func TestResolveSingleCallAgreesWithSharedIndex(t *testing.T) {
	dir := t.TempDir()
	database, err := sqlx.Connect("sqlite", filepath.Join(dir, "cczj_video.db"))
	if err != nil {
		t.Fatal(err)
	}
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	defer func() {
		instance, dataDir = prevInstance, prevDir
		_ = database.Close()
	}()
	if err := createTables(); err != nil {
		t.Fatal(err)
	}

	fixtures := []struct {
		id     int64
		name   string
		typeID int64
		year   string
	}{
		{8001, "阿凡达:水之道", 4, "2022"},
		{8002, "老友记 第六季", 3, "1994"},
	}
	for _, f := range fixtures {
		if _, err := database.Exec(`INSERT INTO global_video (id, vod_name, name_norm, type_id, year) VALUES (?, ?, ?, ?, ?)`,
			f.id, f.name, normalizeTitle(f.name), f.typeID, f.year); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := loadGlobalCandidates(database)
	if err != nil {
		t.Fatal(err)
	}
	index := newGlobalVideoIndex(candidates)

	cases := []struct {
		name     string
		year     string
		typeID   int64
		wantID   int64
		wantTier matchTier
	}{
		{"阿凡达:水之道", "2022", 4, 8001, tierExact},
		{"阿凡达：水之道", "", 4, 8001, tierNorm},  // 全角标点：归一化档
		{"阿凡达:水之道", "", 0, 8001, tierExact}, // 未知类型认领已有行
		{"老友记第六季", "1994", 3, 8002, tierNorm},
		{"权力的游戏 第三季", "", 3, 0, tierNew}, // 谁都不像：新建
	}
	for _, c := range cases {
		fastID, fastTier, err := resolveGlobalVideoID(database, nil, c.name, c.year, c.typeID)
		if err != nil {
			t.Fatalf("fast path %q: %v", c.name, err)
		}
		sharedID, sharedTier, err := resolveGlobalVideoID(database, index, c.name, c.year, c.typeID)
		if err != nil {
			t.Fatalf("shared index %q: %v", c.name, err)
		}
		if c.wantID > 0 {
			if fastID != c.wantID || fastTier != c.wantTier {
				t.Errorf("fast path %q = (%d, %s), want (%d, %s)", c.name, fastID, fastTier, c.wantID, c.wantTier)
			}
		}
		if fastID != sharedID {
			t.Errorf("%q: fast path 得 id=%d，共享索引得 id=%d", c.name, fastID, sharedID)
		}
		if fastTier != c.wantTier || sharedTier != c.wantTier {
			t.Errorf("%q: 阶梯不一致 fast=%s shared=%s want=%s", c.name, fastTier, sharedTier, c.wantTier)
		}
	}
}
