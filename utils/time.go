package utils

import (
	"time"
)

type ExponentialBackoff struct {
	Min          time.Duration
	Max          time.Duration
	current      time.Duration
	previousIncr time.Time
}

func (eb *ExponentialBackoff) Ready() (ready bool, until time.Duration) {
	if eb.previousIncr.IsZero() {
		return true, 0
	}
	whenReady := eb.previousIncr.Add(eb.current)
	ready = time.Now().After(whenReady)
	if !ready {
		until = whenReady.Sub(time.Now())
	}
	return
}

func (eb *ExponentialBackoff) Next() time.Duration {
	if eb.current == 0 {
		eb.current = eb.Min
	}
	current := eb.current
	eb.current *= 2
	if eb.current > eb.Max {
		eb.current = eb.Max
	}
	eb.previousIncr = time.Now()
	return current
}

func (eb *ExponentialBackoff) Wait() {
	time.Sleep(eb.Next())
}

func (eb *ExponentialBackoff) Reset() {
	eb.current = eb.Min
}

type Ticker interface {
	Start()
	Close()
	Notify() <-chan time.Time
}

type StaticTicker struct {
	t        *time.Ticker
	interval time.Duration
}

func NewStaticTicker(interval time.Duration) StaticTicker {
	return StaticTicker{
		t:        time.NewTicker(interval),
		interval: interval,
	}
}

func (t StaticTicker) Start() {
	t.t.Reset(t.interval)
}

func (t StaticTicker) Close() {
	t.t.Stop()
}

func (t StaticTicker) Notify() <-chan time.Time {
	return t.t.C
}

type ExponentialBackoffTicker struct {
	backoff ExponentialBackoff
	chTick  chan time.Time
	chReset chan struct{}
	chStop  chan struct{}
	chDone  chan struct{}
}

func NewExponentialBackoffTicker(min, max time.Duration) *ExponentialBackoffTicker {
	return &ExponentialBackoffTicker{
		backoff: ExponentialBackoff{Min: min, Max: max},
		chTick:  make(chan time.Time),
		chReset: make(chan struct{}),
		chStop:  make(chan struct{}),
		chDone:  make(chan struct{}),
	}
}

func (t *ExponentialBackoffTicker) Start() {
	go func() {
		defer close(t.chDone)
		for {
			func() {
				duration := t.backoff.Next()
				timer := time.NewTimer(duration)
				defer timer.Stop()

				select {
				case <-t.chStop:
					return

				case <-t.chReset:
					t.backoff.Reset()

				case now := <-timer.C:
					select {
					case <-t.chStop:
						return
					case t.chTick <- now:
					}
				}
			}()
		}
	}()
}

func (t *ExponentialBackoffTicker) Reset() {
	select {
	case <-t.chStop:
	case t.chReset <- struct{}{}:
	}
}

func (t *ExponentialBackoffTicker) Close() {
	close(t.chStop)
	<-t.chDone
}

func (t *ExponentialBackoffTicker) Notify() <-chan time.Time {
	return t.chTick
}

// TimeFS is the UTC in 1/1<<16 seconds elapsed since Jan 1, 1970 UTC ("FS" = fractional seconds)
//
// timeFS := TimeNowFS()
//
// Shifting this right 16 bits will yield stanard Unix time.
// This means there are 47 bits dedicated for seconds, implying max timestamp of 4.4 million years.
type TimeFS int64

// TimeNowFS returns the current time (a standard unix UTC timestamp in 1/1<<16 seconds)
func TimeNowFS() TimeFS {
	t := time.Now()

	timeFS := t.Unix() << 16
	frac := uint16((2199 * (uint32(t.Nanosecond()) >> 10)) >> 15)
	return TimeFS(timeFS | int64(frac))
}
