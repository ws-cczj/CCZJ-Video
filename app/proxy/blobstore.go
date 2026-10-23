package proxy

import (
	"context"
	"time"
)

// BlobStore 是 imageStore 的导出外壳，给 proxy 包之外的调用方用。
//
// 之所以是外壳而不是把 imageStore 直接导出：imageStore 的字段（ttl / maxBytes /
// subdir）是给单测调小用的，导出整个类型等于把这些内部旋钮变成公共 API。
// 这里只暴露读写与失败冷却四件事。
//
// 键是调用方给的原始字符串的 sha256，值的首行是内容类型、其后是字节
// （splitBlob 那套格式）。**它不校验内容类型**——校验是调用方的事：
// 图片代理要拒绝 HTML 错误页（verifyImageBytes），正文缓存要拒绝空正文。
// 一个共用的存储层不该替所有调用方决定什么算"合法内容"。
type BlobStore struct {
	inner *imageStore
}

// SetMaintenanceLauncher binds pruning to the application's task owner. Configure
// it before use; standalone stores retain synchronous maintenance for small tools.
func (b *BlobStore) SetMaintenanceLauncher(launch func(string, func(context.Context)) bool) {
	b.inner.maintenance = launch
}

// NewBlobStore 建一份磁盘缓存。dirFn 返回数据目录（延迟求值，因为启动早期
// 数据目录可能还没迁移到位）；subdir 是它在数据目录下的子目录名。
//
// 两份 store 指到同一个 subdir 会各自跑一遍淘汰、互相删对方的文件，
// 所以调用方必须给不同的 subdir。
func NewBlobStore(dirFn func() string, subdir string, ttl time.Duration, maxBytes int64) *BlobStore {
	return &BlobStore{inner: newBlobStore(dirFn, subdir, ttl, maxBytes)}
}

// Read 取回一份还能用的内容。命中会把 mtime 推到当下，于是"最近被看到过"
// 等价于"不会被淘汰"。第二个返回值是写入时记下的内容类型。
func (b *BlobStore) Read(key string) (data []byte, contentType string, ok bool) {
	if b == nil {
		return nil, "", false
	}
	return b.inner.read(key)
}

// Write 原子地写入一份内容：先写临时文件再改名，所以并发读者不会看见半份。
func (b *BlobStore) Write(key string, data []byte, contentType string) {
	if b == nil {
		return
	}
	b.inner.write(key, data, contentType)
}

// Allow 报告这个键是不是还在失败冷却期内。
func (b *BlobStore) Allow(key string) bool {
	if b == nil {
		return true
	}
	return b.inner.allow(key)
}

// NoteFailure 给这个键记一次失败，进入冷却。
func (b *BlobStore) NoteFailure(key string) {
	if b == nil {
		return
	}
	b.inner.noteFailure(key)
}
