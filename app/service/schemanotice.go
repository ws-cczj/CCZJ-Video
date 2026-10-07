package service

import "cczjVideo/app/db"

// SchemaCompat 是「这份库比当前程序新」的现场描述。界面照它说话，不自己比版本号。
//
// 走到这一步只有两条路：拿旧 exe 开了新库，或者「退回上一版」把老程序换回原位。
// 两种都不是数据坏了，而是程序太旧，所以这里只说明情况并给出出口，不拦读写——
// 拦下来的代价是用户连查看自己数据的入口都没了（取舍见 docs/adr/0011-read-newer-library.md）。
type SchemaCompat struct {
	Newer        bool   `json:"newer"`
	DbVersion    int    `json:"db_version"`
	BuildVersion int    `json:"build_version"`
	DataDir      string `json:"data_dir"`
}

// SchemaNotice 把 db 层在启动时记下的比较结果交给界面。
func (a *App) SchemaNotice() SchemaCompat {
	dbVersion, buildVersion, newer := db.DataNewerThanBuild()
	return SchemaCompat{
		Newer:        newer,
		DbVersion:    dbVersion,
		BuildVersion: buildVersion,
		DataDir:      a.getDataDir(),
	}
}
