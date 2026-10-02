package media

import (
	"os"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

// TestMain 把 applog 与数据库都绑到临时目录：applog.Default() 会用 %APPDATA% 的生产
// 目录懒建单例，而这里的失败分支正是要写日志的。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-media-test-")
	if err != nil {
		panic(err)
	}
	if err := applog.Init(dir); err != nil {
		panic(err)
	}
	if err := db.InitDB(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	db.Close()
	applog.Default().Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// seedCatalog 往目录里放一条已归一化到全局身份的视频，返回它自己的源标识。
func seedCatalog(t *testing.T, name string) (sourceKey, vodID string, globalID int64) {
	t.Helper()
	sourceKey = "media_test_" + name
	vodID = "vod-" + name
	if _, err := db.DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	item := &model.Video{
		VodId:    model.FlexibleString(vodID),
		TypeId:   model.FlexibleString("type-" + name),
		TypeName: "Media test type " + name,
		VodName:  "Media test title " + name,
	}
	if err := db.UpsertCatalogItems(sourceKey, []*model.Video{item}); err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetVideoById(sourceKey, vodID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.GlobalId <= 0 {
		t.Fatalf("前置条件不成立：目录条目没有全局身份，GlobalId = %d", stored.GlobalId)
	}
	return sourceKey, vodID, stored.GlobalId
}

// 没有播放记录是正常状态，不是错误。把它当错误抛给界面，用户看到的就是
// 「打开一个没看过的视频先报一次错」。
func TestHistoryPositionWithoutRecordIsZeroNotError(t *testing.T) {
	svc := NewService()
	sourceKey, vodID, _ := seedCatalog(t, "history_empty")

	position, err := svc.HistoryPosition(sourceKey, vodID, 7)
	if err != nil {
		t.Fatalf("无记录不该报错: %v", err)
	}
	if position != 0 {
		t.Fatalf("无记录的位置 = %v, 期望 0", position)
	}
}

func TestSaveAndReadHistoryPosition(t *testing.T) {
	svc := NewService()
	sourceKey, vodID, _ := seedCatalog(t, "history_roundtrip")

	if err := svc.SaveHistory(sourceKey, vodID, 3, 1234.5); err != nil {
		t.Fatal(err)
	}
	position, err := svc.HistoryPosition(sourceKey, vodID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if position != 1234.5 {
		t.Fatalf("回读位置 = %v, 期望 1234.5", position)
	}
	if err := svc.DeleteHistoryItem(sourceKey, vodID, 3); err != nil {
		t.Fatal(err)
	}
	position, err = svc.HistoryPosition(sourceKey, vodID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if position != 0 {
		t.Fatalf("删除后位置 = %v, 期望 0", position)
	}
}

// 收藏是按全局身份共享的：加一次收藏，同身份的另一条目录记录也应当认得出已收藏。
func TestFavoriteRoundTripIsSharedByGlobalIdentity(t *testing.T) {
	svc := NewService()
	sourceKey, vodID, globalID := seedCatalog(t, "favorite")

	if ok, err := svc.IsFavorite(sourceKey, vodID); err != nil || ok {
		t.Fatalf("初始状态应为未收藏: ok=%v err=%v", ok, err)
	}
	if err := svc.AddFavorite(sourceKey, vodID); err != nil {
		t.Fatal(err)
	}
	if ok, err := svc.IsFavorite(sourceKey, vodID); err != nil || !ok {
		t.Fatalf("收藏后应读到已收藏: ok=%v err=%v", ok, err)
	}

	found := false
	page, err := svc.Favorites(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, fav := range page {
		if int64(fav.GlobalID) == globalID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("收藏列表里没有 global_id=%d 这一条", globalID)
	}

	if err := svc.RemoveFavorite(sourceKey, vodID); err != nil {
		t.Fatal(err)
	}
	if ok, err := svc.IsFavorite(sourceKey, vodID); err != nil || ok {
		t.Fatalf("取消收藏后应为未收藏: ok=%v err=%v", ok, err)
	}
}

// 目录里查不到条目时必须是错误，不能静默当成 global_id=0 去写收藏——
// 那正是「界面说收藏成功了，重启后哪一档里都没有」的形状。
func TestFavoriteOnUnknownVideoIsAnError(t *testing.T) {
	svc := NewService()
	sourceKey, _, _ := seedCatalog(t, "favorite_missing")

	err := svc.AddFavorite(sourceKey, "vod-does-not-exist")
	if err == nil {
		t.Fatal("目录缺条目时新增收藏必须报错")
	}
	if apperror.CodeOf(err) != apperror.NotFound {
		t.Fatalf("错误分类 = %v, 期望 NotFound", apperror.CodeOf(err))
	}
}

// WatchedEpisodes 走同一道身份闸门：查不到身份就报错，不返回空列表装作「还没看」。
func TestWatchedEpisodesOnUnknownVideoIsAnError(t *testing.T) {
	svc := NewService()
	sourceKey, _, _ := seedCatalog(t, "watched_missing")

	if _, err := svc.WatchedEpisodes(sourceKey, "vod-does-not-exist"); err == nil {
		t.Fatal("目录缺条目时读取已看集数必须报错")
	}
}

// IsFavorite 面对查不到的视频要答「没收藏」而不是报错：这是每次渲染卡片都会走的路径。
func TestIsFavoriteOnUnknownVideoIsFalseNotError(t *testing.T) {
	svc := NewService()
	sourceKey, _, _ := seedCatalog(t, "isfav_missing")

	ok, err := svc.IsFavorite(sourceKey, "vod-does-not-exist")
	if err != nil {
		t.Fatalf("查不到的视频不该报错: %v", err)
	}
	if ok {
		t.Fatal("查不到的视频不能算已收藏")
	}
}
