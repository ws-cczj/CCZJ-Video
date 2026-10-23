package model

// LibraryItem 是小说或漫画的一部作品。
//
// 与视频侧的 global_video 不同，这里**没有跨源归一**：同一本书从两个源加进来就是两条
// 记录、两份进度。理由是阅读没有"换源续播"这个需求，而按书名合并会把不同译本、不同
// 版本、不同扫描组合成一条（reports/00:107、ADR 0016）。
type LibraryItem struct {
	Id        int    `json:"id" db:"id"`
	Kind      string `json:"kind" db:"kind"`
	SourceKey string `json:"source_key" db:"source_key"`
	// RemoteID 是规范化后的去域名路径。规范化比 Mihon 的 setUrlWithoutDomain 多走一步：
	// 剥掉 fragment、query 键排序，这样一个追加轮换签名参数的源不会让每章都变成新章。
	RemoteID string `json:"remote_id" db:"remote_id"`
	// RemoteURL 是这一条在源那边的**原始绝对地址**。
	//
	// 它与 RemoteID 是两件事，不能互相推导：RemoteID 是规范化后的身份
	// （去域名、剥 fragment、query 排序），而请求要发的是原始地址。
	// 源文档可以声明显式的 id 字段，那时 RemoteID 是 "12345" 这种值，
	// 拿它去拼 api_url 会得到一个纯垃圾地址；而详情页也可能在另一个域上，
	// 从 api_url 推不回来。所以地址必须单独存一列。
	RemoteURL string `json:"remote_url" db:"remote_url"`

	Title   string `json:"title" db:"title"`
	Author  string `json:"author" db:"author"`
	Cover   string `json:"cover" db:"cover"`
	Summary string `json:"summary" db:"summary"`
	Genres  string `json:"genres" db:"genres"`
	Status  string `json:"status" db:"status"`
	// Latest 是源给出的最新章节名，List 用；Words 是字数，小说用。
	Latest string `json:"latest" db:"latest"`
	Words  string `json:"words" db:"words"`
	// RemoteUpdatedAt 是源声明的更新时间原文。单位与格式由源决定，不在这一层换算：
	// reports/04:24 记过 fqapp 的 video_duration 是秒、Mihon 的 date_upload 是 Long，
	// 混着猜会差一千倍。展示层要格式化就自己解析，解析不了就原样显示。
	RemoteUpdatedAt string `json:"remote_updated_at" db:"remote_updated_at"`

	// LifecycleState 是 active 或 deleted。源不再返回的条目转 deleted 而不是删行：
	// 用户的书架与进度还指着它，删行会把"这本书读不下去了"变成"这本书从来没存在过"。
	LifecycleState string `json:"lifecycle_state" db:"lifecycle_state"`

	CreatedAt string `json:"created_at" db:"created_at"`
	UpdatedAt string `json:"updated_at" db:"updated_at"`
}

// ChapterAccess 描述一章在源那边**声明**的访问条件。它是元数据，不等于内容已经拿到手：
// 标着 available 的章节也可能因为源改版而取不到正文（reports/02:80）。
//
// 这里刻意不放"加载失败/被限流/解析不了"那一类值——那是运行时结果，属于 apperror 的
// 编码错误体系（AGENTS.md），与源声明的访问条件是两套东西，混在一列里会让"这章要登录"
// 和"这章刚才没抓下来"变成同一个状态，界面上就分不清该提示用户还是该重试。
type ChapterAccess string

const (
	ChapterAccessAvailable    ChapterAccess = "available"
	ChapterAccessLogin        ChapterAccess = "login"
	ChapterAccessSubscription ChapterAccess = "subscription"
	ChapterAccessUnknown      ChapterAccess = "unknown"
)

// Valid 报告这个值是不是四种之一。源文档里写错的访问条件按 unknown 处理，
// 不能让一个拼错的字符串变成界面上一个谁也不认的徽章。
func (a ChapterAccess) Valid() bool {
	switch a {
	case ChapterAccessAvailable, ChapterAccessLogin, ChapterAccessSubscription, ChapterAccessUnknown:
		return true
	}
	return false
}

// NormalizeAccess 把空值与非法值收敛成 unknown。
func NormalizeAccess(a ChapterAccess) ChapterAccess {
	if a.Valid() {
		return a
	}
	return ChapterAccessUnknown
}

// LibraryChapter 是目录里的一章（小说）或一话（漫画）。
type LibraryChapter struct {
	Id       int    `json:"id" db:"id"`
	ItemID   int    `json:"item_id" db:"item_id"`
	RemoteID string `json:"remote_id" db:"remote_id"`
	// RemoteURL 见 LibraryItem.RemoteURL：身份与地址是两件事，
	// 章节的地址同样不能从 remote_id 反推。
	RemoteURL string `json:"remote_url" db:"remote_url"`
	Title     string `json:"title" db:"title"`
	// Ord 是展示序，由我们按源声明的方向算出来后写入，**不是远端列表下标**。
	// Mihon 的 sourceOrder 直接取远端下标，并在注释里要求源"必须从新到旧返回"，
	// 且不做方向检测（SyncChaptersWithSource.kt:195-196）；倒序源会让它的语义整个反转。
	// 我们由源文档显式声明方向，倒序在这里被翻正，Ord 一律是"第几章"。
	Ord int `json:"ord" db:"ord"`
	// Volume 是卷名。IsVolume 为真时这一行是卷标题而不是章节，没有正文，
	// 判空逻辑必须豁免它（Legado 的 ContentEmptyException 就有这条豁免，BookContent.kt:149）。
	Volume   string `json:"volume" db:"volume"`
	IsVolume bool   `json:"is_volume" db:"is_volume"`

	Access ChapterAccess `json:"access" db:"access"`
	// RemoteUploadedAt 同 LibraryItem.RemoteUpdatedAt：源给的原文，不在这一层换算单位。
	RemoteUploadedAt string `json:"remote_uploaded_at" db:"remote_uploaded_at"`

	LifecycleState string `json:"lifecycle_state" db:"lifecycle_state"`

	CreatedAt string `json:"created_at" db:"created_at"`
	UpdatedAt string `json:"updated_at" db:"updated_at"`
}

// LibraryProgress 是一本书的阅读位置。小说记章内相对位置（0..1），漫画记页与页内偏移。
//
// 它现在只活在前端 localStorage 的 v2 键里（ADR 0014 / ADR 0016），这个结构是两边共用
// 的形状：前端存它、后端接口也按它说话，将来若要搬进 SQLite（pending-decisions 第 21 条）
// 不需要改字段语义。
type LibraryProgress struct {
	// ChapterOrd 是我们算出来的展示序，不是远端下标，也不是数组位置。
	ChapterOrd int `json:"chapterOrd"`
	// Ratio 是章内相对位置，小说分页与滚动模式都用它。字号或视口变化后按它重新分页，
	// 这是相对位置恢复，不是逐字锚点。
	Ratio float64 `json:"ratio"`
	// Page 与 PageOffset 只有漫画用：页序号与页内滚动偏移。
	Page       int     `json:"page,omitempty"`
	PageOffset float64 `json:"pageOffset,omitempty"`
	UpdatedAt  int64   `json:"updatedAt"`
}
