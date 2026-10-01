package handler

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"time"
)

// 采集源的失效判定与自动停用。
//
// 健康度样本表建好之后，"这个源其实已经连续失败二十次了"一直是看得见的，
// 只是没人据此做任何事：源照样 enabled=1，调度照样按它的计划去戳一个已经下线的
// 接口，用户要等到采集列表空转才发现。这里把观测面接回执行面——连败到阈值就停用，
// 并在日志里写清是哪一类失败、最后一条错误是什么。
//
// 停用是可逆的：采集源页把它重新打开即可，重开的同时旧样本作废
// （见 UpdateSource），不会被上一次的失败立刻再次停用。

// SourceDeadStreak 是同一类观测连败多少次算"这个源已经废了"。
// 一次失败可能是网络抖动，三次意味着至少跨过了完整的一轮重试，值得动手。
const SourceDeadStreak = 3

// sourceRecoverCooldown 是被自动停用的源隔多久重新放回采集池。
//
// 自动停用不能是单向门：CMS 源下线检修一晚上很常见，没有冷却的话它从此不再被采集，
// 用户不点开采集源页就永远发现不了自己的库停止更新了。24 小时和"一天一次全量补采"
// 的节奏同量级——代价是每天最多为死源多花三次取页，这正是自动停用之前一直在做的事。
const sourceRecoverCooldown = 24 * time.Hour

// RecoverAutoDisabledSources 把冷却到期的自动停用源放回采集池，由调度器启动时调用。
// 返回恢复的源数量，供调度日志写一句"今天又给了谁一次机会"。
func RecoverAutoDisabledSources() int {
	keys, err := db.RecoverAutoDisabledSources(time.Now().Add(-sourceRecoverCooldown))
	if err != nil {
		applog.Warn("[Source] 恢复到期自动停用源失败: %v", err)
		return 0
	}
	for _, key := range keys {
		applog.Info("[Source] 采集源 %s 自动停用已满 %v，重新放回采集池", key, sourceRecoverCooldown)
	}
	return len(keys)
}

// RecordSourceHealthSample 落一条源健康度样本，并在失败连败到阈值时停用该源。
//
// 记账失败直接返回：自动停用建立在"这条样本确实进库了"之上，写不进去时
// 连败计数是旧的，据此停用会冤枉一个刚恢复的源。
func RecordSourceHealthSample(sourceKey, kind string, ok bool, latencyMS int64, saved int, errText string) {
	if err := db.RecordSourceHealth(sourceKey, kind, ok, latencyMS, saved, errText, time.Now()); err != nil {
		applog.Warn("[Source] 记录源健康度失败（不影响本次运行）: %v", err)
		return
	}
	if ok {
		return
	}
	disableDeadSource(sourceKey, kind)
}

// disableDeadSource 按该类观测的连败数决定是否停用。
func disableDeadSource(sourceKey, kind string) {
	// window 传 0：连败必须按保留的全部历史算，只看最近几条会把"上周开始一直红"
	// 读成"今天刚红了两次"。
	health, err := db.GetSourceHealth(sourceKey, kind, 0)
	if err != nil {
		applog.Warn("[Source] 读取 %s 的健康度汇总失败: %v", sourceKey, err)
		return
	}
	if health.FailStreak < SourceDeadStreak {
		return
	}
	changed, err := db.AutoDisableSource(sourceKey, time.Now())
	if err != nil {
		applog.Warn("[Source] 自动停用 %s 失败: %v", sourceKey, err)
		return
	}
	// 已经是停用状态（用户先手动关过）时不重复播报。
	if !changed {
		return
	}
	applog.Warn("[Source] 采集源 %s 连续 %d 次%s失败，已自动停用；最后错误：%s。在采集源页重新启用即可恢复。",
		sourceKey, health.FailStreak, kindLabel(kind), firstNonEmpty(health.LastError, "无"))
}

func kindLabel(kind string) string {
	if kind == db.SourceHealthKindPatrol {
		return "巡检"
	}
	return "采集"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
