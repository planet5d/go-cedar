package process

import (
	"context"
	"fmt"
	"time"
)

type PoolWorker interface {
	Context
	Add(item PoolWorkerItem)
	ForceRetry(uniqueID PoolUniqueID)
}

type PoolWorkerItem interface {
	PoolUniqueIDer
	Work(ctx context.Context) (retry bool)
}

type PoolWorkerScheduler interface {
	CheckForRetriesInterval() time.Duration
	RetryWhen(item PoolWorkerItem) time.Time
}

type poolWorker struct {
	Process
	name        string
	concurrency int
	pool        *Pool
	scheduler   PoolWorkerScheduler
}

func NewPoolWorker(name string, concurrency int, scheduler PoolWorkerScheduler) *poolWorker {
	return &poolWorker{
		name:        name,
		concurrency: concurrency,
		pool:        NewPool(name, concurrency, scheduler.CheckForRetriesInterval()),
		scheduler:   scheduler,
	}
}

func (w *poolWorker) OnStart() error {
	err := w.StartChild(w.pool, "poolWorker")
	if err != nil {
		return err
	}

	for i := 0; i < w.concurrency; i++ {
		w.Process.Go(fmt.Sprintf("worker %v", i), func(ctx Context) {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				x, err := w.pool.Get(ctx)
				if err != nil {
					return
				}
				item := x.(PoolWorkerItem)

				retry := item.Work(ctx)
				if retry {
					w.pool.RetryLater(item.ID(), w.scheduler.RetryWhen(item))
				} else {
					w.pool.Complete(item.ID())
				}
			}
		})
	}
	return nil
}

func (w *poolWorker) Close() {
	w.Process.Close()
	w.pool.Close()
}

func (w *poolWorker) Add(item PoolWorkerItem) {
	w.pool.Add(item)
}

func (w *poolWorker) ForceRetry(id PoolUniqueID) {
	w.pool.ForceRetry(id)
}

type StaticScheduler struct {
	checkForRetriesInterval time.Duration
	retryAfter              time.Duration
}

var _ PoolWorkerScheduler = StaticScheduler{}

func NewStaticScheduler(checkForRetriesInterval time.Duration, retryAfter time.Duration) StaticScheduler {
	return StaticScheduler{checkForRetriesInterval, retryAfter}
}

func (s StaticScheduler) CheckForRetriesInterval() time.Duration { return s.checkForRetriesInterval }
func (s StaticScheduler) RetryWhen(item PoolWorkerItem) time.Time {
	return time.Now().Add(s.retryAfter)
}
