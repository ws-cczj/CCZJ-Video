package applog

import (
	"strings"
	"time"
)

// DefaultRingSize 是内存日志环形缓冲的容量。
// 它只保留最近的若干条，用于界面实时展示与增量拉取；落盘文件仍是权威来源。
const DefaultRingSize = 5000

// Record 是一条已落盘日志的结构化视图，字段顺序与文件行格式一一对应。
type Record struct {
	Seq     uint64 `json:"seq"`
	TS      int64  `json:"ts"` // unix 毫秒
	Level   string `json:"level"`
	Caller  string `json:"caller"`
	Message string `json:"message"`
}

// Subscribe 注册一个实时日志订阅者。返回的通道容量有限，
// 消费不过来时丢弃新记录并累加 Dropped，绝不阻塞写日志的调用方。
// cancel 必须被调用以释放通道。
func (l *Logger) Subscribe() (<-chan Record, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.subs == nil {
		l.subs = make(map[int]chan Record)
	}
	l.subID++
	id := l.subID
	ch := make(chan Record, subscribeChannelSize)
	l.subs[id] = ch
	return ch, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if existing, ok := l.subs[id]; ok && existing == ch {
			delete(l.subs, id)
			close(ch)
		}
	}
}

const subscribeChannelSize = 256

// Snapshot 返回环形缓冲中的全部记录，按时间从旧到新。
func (l *Logger) Snapshot() []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.orderedLocked(0, 0)
}

// Since 返回 Seq 大于 since 的记录（旧→新）。limit 大于 0 时只保留最新的 limit 条。
func (l *Logger) Since(since uint64, limit int) []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.orderedLocked(since, limit)
}

// LatestSeq 返回最后一条记录的序号；缓冲为空时返回 0。
func (l *Logger) LatestSeq() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.latestSeqLocked()
}

// RingLen 返回当前缓冲内的记录条数。
func (l *Logger) RingLen() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

// RingCap 返回环形缓冲容量。
func (l *Logger) RingCap() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ring) == 0 {
		return DefaultRingSize
	}
	return len(l.ring)
}

// Dropped 返回因订阅者来不及消费而被丢弃的记录数。
func (l *Logger) Dropped() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dropped
}

// SetMinLevel 设置最低日志级别，可运行时调用并立即生效。
func SetMinLevel(level Level) {
	if l := Default(); l != nil {
		l.mu.Lock()
		l.minLevel = level
		l.mu.Unlock()
	}
}

// MinLevel 返回当前最低日志级别。
func MinLevel() Level {
	l := Default()
	if l == nil {
		return LevelInfo
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.minLevel
}

// LevelName 返回当前最低级别的字符串形式，供界面展示。
func LevelName() string { return MinLevel().String() }

// IsDebug 返回是否处于调试模式
func IsDebug() bool { return MinLevel() <= LevelDebug }

func (l *Logger) latestSeqLocked() uint64 {
	if l.count == 0 {
		return 0
	}
	idx := (l.head - 1) % len(l.ring)
	if idx < 0 {
		idx += len(l.ring)
	}
	return l.ring[idx].Seq
}

// orderedLocked 从旧到新拷贝缓冲内容，可按 Seq 过滤并截取最新的 limit 条。
func (l *Logger) orderedLocked(since uint64, limit int) []Record {
	if l.count == 0 {
		return nil
	}
	n := len(l.ring)
	start := l.head - l.count
	out := make([]Record, 0, l.count)
	for i := 0; i < l.count; i++ {
		idx := (start + i) % n
		if idx < 0 {
			idx += n
		}
		rec := l.ring[idx]
		if rec.Seq > since {
			out = append(out, rec)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// pushLocked 把一条已写盘的记录放入环形缓冲并广播给订阅者。调用方需持有 l.mu。
func (l *Logger) pushLocked(level Level, ts time.Time, caller, msg string) {
	if len(l.ring) == 0 {
		l.ring = make([]Record, DefaultRingSize)
	}
	l.seq++
	rec := Record{
		Seq:     l.seq,
		TS:      ts.UnixMilli(),
		Level:   level.String(),
		Caller:  caller,
		Message: msg,
	}
	n := len(l.ring)
	l.ring[l.head%n] = rec
	l.head++
	if l.count < n {
		l.count++
	}
	for _, ch := range l.subs {
		select {
		case ch <- rec:
		default:
			l.dropped++
		}
	}
}

// ParseRecordLine 把一行日志文件内容还原成结构化记录，供历史文件浏览使用。
// 支持两种既有格式：带调用位置和不带调用位置。无法解析时返回 false。
func ParseRecordLine(line string) (Record, bool) {
	line = strings.TrimRight(strings.TrimPrefix(line, "\uFEFF"), "\r\n")
	tsText, rest, ok := bracketField(line)
	if !ok {
		return Record{}, false
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04:05.000", tsText, time.Local)
	if err != nil {
		return Record{}, false
	}
	levelText, rest, ok := bracketField(rest)
	if !ok {
		return Record{}, false
	}
	level := strings.ToUpper(levelText)
	if _, known := parseLevel(level); !known {
		return Record{}, false
	}
	rec := Record{TS: ts.UnixMilli(), Level: level}
	if caller, tail, ok := bracketField(rest); ok {
		rec.Caller = caller
		rec.Message = strings.TrimPrefix(tail, " ")
		return rec, true
	}
	rec.Message = rest
	return rec, true
}

// bracketField 读取开头形如 "[内容]" 的字段，返回内容与剩余文本。
// 写盘用的是 "[...] [...] [...]" 连续括号字段，按 ']' 直接切会把左括号留在
// 下一段里，所以成对读取而不是 split。
func bracketField(s string) (value, rest string, ok bool) {
	s = strings.TrimPrefix(s, " ")
	if !strings.HasPrefix(s, "[") {
		return "", s, false
	}
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return "", s, false
	}
	return s[1:end], s[end+1:], true
}

func parseLevel(name string) (Level, bool) {
	switch name {
	case "DEBUG":
		return LevelDebug, true
	case "INFO":
		return LevelInfo, true
	case "WARN":
		return LevelWarn, true
	case "ERROR":
		return LevelError, true
	}
	return LevelInfo, false
}
