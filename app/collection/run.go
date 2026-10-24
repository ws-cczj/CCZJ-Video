package collection

import (
	"context"
	"fmt"

	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
	"cczjVideo/app/collect"
	"cczjVideo/app/operation"
)

func runContext(owner, request context.Context, key string) (context.Context, string) {
	ctx, id := operation.OwnedContext(owner, request, "collection.run", applog.Fields{"mode": "video", "source_key": key})
	return operation.WithFields(ctx, applog.Fields{"operation_id": id.RequestID}), id.RequestID
}

// execute owns all post-run work. Finishing the registry entry is the final
// storage boundary, so source mutations can safely wait on that entry.
func (s *Service) execute(engine *collect.Engine, entry *engineEntry, key, mode, id string) {
	outcome := RunOutcome{}
	defer func() {
		if failure := recover(); failure != nil {
			applog.ErrorFields("collection panic", applog.Fields{"operation_id": id, "source_key": key, "failure": fmt.Sprint(failure)})
			outcome.Log = "采集运行异常，请查看诊断日志"
			outcome.ErrorKind = "internal"
		}
		entry.FinishEngine(engine, outcome)
		payload := CollectDonePayload(key, mode, outcome)
		payload["operation_id"] = id
		s.emit("collect:done", payload)
	}()
	stats, err := engine.Run()
	outcome = OutcomeFromRun(stats, err)
	RecordCollectHealth(key, outcome, err)
	if outcome.Saved > 0 {
		cache.InvalidateSource(key, fmt.Sprintf("采集写入 %d 条", outcome.Saved))
	}
	applog.InfoFields("collection finished", applog.Fields{"operation_id": id, "source_key": key, "mode": mode, "saved": outcome.Saved, "error_kind": outcome.ErrorKind, "error": outcome.Log})
}
