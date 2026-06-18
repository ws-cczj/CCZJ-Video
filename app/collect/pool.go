package collect

import (
	"sync"
)

// Pool 固定大小的协程池，用于并发执行详情补充等 I/O 密集型任务。
// 特点：
//   - 固定 worker 数，任务通过 channel 分发
//   - channel 满时同步回退（背压保护，不会无限堆积任务）
//   - 支持优雅停止（等待所有任务完成）和立即停止
type Pool struct {
	workers  int
	taskCh   chan func()
	wg       sync.WaitGroup
	stopOnce sync.Once
	done     chan struct{} // 关闭后 worker 退出
}

// NewPool 创建协程池。workers 为并发数，建议 SQLite 写场景设 2-4，纯 HTTP 场景设 3-8。
func NewPool(workers int) *Pool {
	if workers <= 0 {
		workers = 2
	}
	p := &Pool{
		workers: workers,
		taskCh:  make(chan func(), workers*4),
		done:    make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		go p.worker()
	}
	return p
}

func (p *Pool) worker() {
	for {
		select {
		case fn, ok := <-p.taskCh:
			if !ok {
				return
			}
			fn()
			p.wg.Done()
		case <-p.done:
			// 退出前把 channel 里剩余任务处理完
			for {
				select {
				case fn, ok := <-p.taskCh:
					if !ok {
						return
					}
					fn()
					p.wg.Done()
				default:
					return
				}
			}
		}
	}
}

// Submit 提交任务。如果池已停止或 channel 满则在当前协程同步执行（背压保护）。
func (p *Pool) Submit(fn func()) {
	p.wg.Add(1)
	select {
	case p.taskCh <- fn:
		// 成功入队
	default:
		// channel 满了，同步执行
		fn()
		p.wg.Done()
	}
}

// Wait 等待所有已提交任务完成。
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Stop 优雅停止：等待所有已提交任务完成后关闭 worker。
func (p *Pool) Stop() {
	p.stopOnce.Do(func() {
		p.wg.Wait()
		close(p.done)
		close(p.taskCh)
	})
}

// StopImmediate 立即停止：不再等待未开始的任务，尽快退出。
func (p *Pool) StopImmediate() {
	p.stopOnce.Do(func() {
		close(p.done)
		close(p.taskCh)
	})
}
