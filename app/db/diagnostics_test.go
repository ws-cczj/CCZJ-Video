package db

import (
	"strings"
	"testing"
)

// 这个查询只 SELECT，靠 sqlx 把列名映射到字段。少了 db tag 时编译和 go vet 都发现不了，
// 只有真机打开诊断页才会显示"查询失败"，所以在这里钉住四个列。
func TestListDoubanDuplicateGroupsMapsEveryColumn(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const doubanID = "diag-dup-36429982"
	if _, err := DB().Exec(`DELETE FROM global_video WHERE douban_id=?`, doubanID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = DB().Exec(`DELETE FROM global_video WHERE douban_id=?`, doubanID)
	}()

	if _, err := DB().Exec(
		`INSERT INTO global_video (vod_name, douban_id, douban_score) VALUES (?, ?, ?), (?, ?, ?)`,
		"诊断归一化甲", doubanID, "8.1",
		"诊断归一化乙", doubanID, "",
	); err != nil {
		t.Fatal(err)
	}

	groups, err := ListDoubanDuplicateGroups(50)
	if err != nil {
		t.Fatal(err)
	}
	var found *DoubanDuplicateGroup
	for i := range groups {
		if groups[i].DoubanID == doubanID {
			found = &groups[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("ListDoubanDuplicateGroups() 没有返回种下的分组 %s", doubanID)
	}
	if found.Rows != 2 || found.Missing != 1 {
		t.Errorf("rows/missing = %d/%d, 期望 2/1", found.Rows, found.Missing)
	}
	if !strings.Contains(found.Names, "诊断归一化甲") || !strings.Contains(found.Names, "诊断归一化乙") {
		t.Errorf("names = %q, 期望包含两个标题", found.Names)
	}
}
