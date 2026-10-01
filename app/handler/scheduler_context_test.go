package handler

import (
	"context"
	"testing"
	"time"
)

// 调度器的取消通道必须在 Start 之前由应用生命周期注入。谁先调用 GetScheduler
// 谁定死上下文，等于让"退出时能不能取消采集"取决于用户先点了哪个按钮。
func TestSchedulerAdoptsApplicationContextOnlyBeforeStart(t *testing.T) {
	s := &Scheduler{ctx: context.Background(), sourceTimers: make(map[string]*time.Timer)}
	if s.appContext().Done() != nil {
		t.Fatal("新建调度器不该已经挂在可取消的上下文上")
	}

	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	s.SetContext(appCtx)
	if s.appContext() != appCtx {
		t.Fatalf("SetContext 后上下文 = %v, want 应用上下文", s.appContext())
	}

	// 已经在跑的循环按旧上下文判断取消，中途换掉会让那一轮的取消依据失效。
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()
	otherCtx, cancelOther := context.WithCancel(context.Background())
	defer cancelOther()
	s.SetContext(otherCtx)
	if s.appContext() != appCtx {
		t.Fatalf("调度器运行中换了上下文: %v", s.appContext())
	}
}

func TestSchedulerSetContextIgnoresNil(t *testing.T) {
	s := &Scheduler{ctx: context.Background()}
	s.SetContext(nil)
	if s.appContext() == nil {
		t.Fatal("SetContext(nil) 不该把上下文清空")
	}
}
