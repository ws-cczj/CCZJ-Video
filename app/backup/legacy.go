package backup

import (
	"os"
	"path/filepath"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"

	"github.com/jmoiron/sqlx"
)

// 2.1.0 把数据目录从「exe 旁边的 data」搬进了用户配置目录，却没有把库搬过去。
// 后来补上的迁移只在「新目录还没有库」时动手，而新目录的库总在更新之前就被首次
// 启动建好了（热榜预加载就会往里写）。结果旧库永远留在原地，用户每次升级看到的
// 都是一个空库。这里负责把那份旧数据读回来，走现有备份的合并引擎并进当前库：
// 只补不删，旧库一个字节都不改，读错了还能再读一次。

// legacyDBName 是历代版本共用的数据库文件名。
const legacyDBName = "cczj_video.db"

// LegacyCounts 是旧库里能找回的内容量。界面按它决定说什么话：
// 三项全零的旧库不值得弹提示。
type LegacyCounts struct {
	Favorites int `json:"favorites"`
	History   int `json:"history"`
	Sources   int `json:"sources"`
}

// Empty 表示旧库里没有任何可找回的用户内容。
func (c LegacyCounts) Empty() bool {
	return c.Favorites == 0 && c.History == 0 && c.Sources == 0
}

// PeekLegacy 只数旧库里的收藏、历史与采集源，不碰当前库。
// 提示框要先有据可依，才谈得上让用户点合并。
func (s *Service) PeekLegacy(legacyDir string) (LegacyCounts, error) {
	var counts LegacyCounts
	err := withLegacyDB(legacyDir, func(database *sqlx.DB) error {
		// 认不出的旧库（没有这些表）在这里就报错：那已经说明它不是本应用的库，
		// 后面的合并也不会有结果，不如把原话交给界面。
		if err := database.QueryRow(`SELECT count(*) FROM favorites`).Scan(&counts.Favorites); err != nil {
			return err
		}
		if err := database.QueryRow(`SELECT count(*) FROM watch_history`).Scan(&counts.History); err != nil {
			return err
		}
		return database.QueryRow(`SELECT count(*) FROM sources`).Scan(&counts.Sources)
	})
	return counts, err
}

// MergeLegacy 把旧库里的采集源、收藏、历史以及它们指向的元数据并进当前库。
// 旧库保持只读；合并是追加，本机已有的值一律保留。
func (s *Service) MergeLegacy(legacyDir string) (Result, error) {
	payload, err := readLegacyPayload(legacyDir)
	if err != nil {
		return Result{}, err
	}
	counts := LegacyCounts{Favorites: len(payload.Favorites), History: len(payload.History), Sources: len(payload.Sources)}
	if counts.Empty() {
		return Result{}, apperror.New(apperror.NotFound, "旧库里没有可找回的收藏、历史或采集源")
	}
	return s.Import(payload, "legacy:"+legacyDir)
}

// readLegacyPayload 逐项读旧库，某一项读不出只丢那一项：2.0.x 的库里可能没有
// 后来才加的列，为了「海报元数据读不出」把收藏和历史一起放弃是不划算的。
// 全都读不出才算失败。
//
// 设置一概不读：那是一份可能停在几个版本之前的偏好，把用户现在的主题、数据新鲜度
// 之类盖掉，比少带几项设置糟得多。
func readLegacyPayload(legacyDir string) (Payload, error) {
	payload := Payload{
		Kind:     payloadKind,
		Version:  payloadVersion,
		Exported: time.Now().Format(time.RFC3339),
	}
	var failures []string
	err := withLegacyDB(legacyDir, func(database *sqlx.DB) error {
		if rows, readErr := db.ReadFavorites(database); readErr != nil {
			failures = append(failures, "favorites: "+readErr.Error())
		} else {
			payload.Favorites = rows
		}
		if rows, readErr := db.ReadHistory(database); readErr != nil {
			failures = append(failures, "history: "+readErr.Error())
		} else {
			payload.History = rows
		}
		if rows, readErr := db.ReadLinkedVideoMeta(database); readErr != nil {
			failures = append(failures, "metadata: "+readErr.Error())
		} else {
			payload.Videos = rows
		}
		if rows, readErr := db.ReadSources(database); readErr != nil {
			failures = append(failures, "sources: "+readErr.Error())
		} else {
			payload.Sources = rows
		}
		if len(payload.Favorites)+len(payload.History)+len(payload.Sources)+len(payload.Videos) == 0 && len(failures) > 0 {
			return apperror.Newf(apperror.Corrupt, "旧库读不出任何内容（%v）", failures)
		}
		return nil
	})
	if err != nil {
		return Payload{}, err
	}
	for _, failure := range failures {
		applog.Warn("[Legacy] 旧库这一部分读不出，已跳过: %s", failure)
	}
	return payload, nil
}

// withLegacyDB 以只读方式打开旧数据目录里的库。
//
// 先原地打开：旧库可能有几百 MB，为了给合并读几张表先整份复制一遍，界面会当场
// 卡住。原地打不开（目录只读、或 -wal 待恢复却没处写 -shm）才退回临时副本——
// 与归档恢复同一条底线：绝不改用户那份旧库。
func withLegacyDB(legacyDir string, fn func(database *sqlx.DB) error) error {
	if legacyDir == "" {
		return apperror.New(apperror.Validation, "没有旧数据目录可读")
	}
	path := filepath.Join(legacyDir, legacyDBName)
	info, err := os.Stat(path)
	if err != nil {
		return apperror.Wrap(apperror.NotFound, err, "旧数据目录里没有数据库")
	}
	if info.IsDir() || info.Size() == 0 {
		return apperror.New(apperror.Corrupt, "旧数据库是个空文件")
	}

	database, err := sqlx.Connect("sqlite", path+"?_pragma=busy_timeout(3000)&_pragma=query_only(TRUE)")
	if err == nil {
		defer database.Close()
		return fn(database)
	}
	applog.Warn("[Legacy] 旧库无法原地只读打开，改用临时副本: %v", err)

	copied, closeCopy, copyErr := readOnlyCopy(path)
	if copyErr != nil {
		return copyErr
	}
	defer closeCopy()
	return fn(copied)
}
