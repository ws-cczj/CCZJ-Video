package collection

import (
	"cczjVideo/app/lifecycle"
	"context"
)

var testRegistry = newRegistry()
var testTasks = lifecycle.NewGroup()

func GetOrCreateEngine(key string) *engineEntry  { return testRegistry.GetOrCreateEngine(key) }
func GetCollectStatus(key string) *CollectStatus { return testRegistry.GetCollectStatus(key) }
func PauseCollect(key string) bool               { return testRegistry.PauseCollect(key) }
func ResumeCollect(key string) bool              { return testRegistry.ResumeCollect(key) }
func StopCollect(key string) bool                { return testRegistry.StopCollect(key) }
func testRuns() *Service                         { return NewService(nil, testTasks.Go, context.Background()) }
