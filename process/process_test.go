package process_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/planet5d/go-cedar/process"
	"github.com/planet5d/go-cedar/testutils"
)

func spawnN(p process.Context, numGoroutines int, delay time.Duration) {
	for i := 0; i < numGoroutines; i++ {
		name := fmt.Sprintf("#%d", i+1)
		p.Go(name, func(ctx process.Context) {
			time.Sleep(delay)
			yoyo := delay
			fmt.Print(yoyo)
		})
	}
}

func TestProcess(t *testing.T) {
	t.Run("it does a thing", func(t *testing.T) {
		p := process.New("")
		p.Start()

		spawnN(p, 3, 1*time.Second)

		p.Autoclose()

		select {
		case <-time.After(5 * time.Second):
			t.Fatal("fail")
		case <-p.Done():
		}
	})

	t.Run("it does a thing", func(t *testing.T) {
		p := process.New("")
		p.Start()

		spawnN(p, 3, 1*time.Second)

		select {
		case <-time.After(5 * time.Second):
		case <-p.Done():
			t.Fatal("fail")
		}

		p.Autoclose()

		select {
		case <-time.After(5 * time.Second):
			t.Fatal("fail")
		case <-p.Done():
		}
	})

	t.Run("child", func(t *testing.T) {
		p := process.New("")
		p.Start()
		defer p.Close()

		child := process.New("child")
		p.StartChild(child)
		spawnN(child, 3, 1*time.Second)

		child.Autoclose()

		select {
		case <-time.After(5 * time.Second):
			t.Fatal("fail")
		case <-child.Done():
		}
	})

	t.Run("child", func(t *testing.T) {
		p := process.New("")
		p.Start()
		defer p.Close()

		child := process.New("child")
		p.StartChild(child)
		spawnN(child, 3, 1*time.Second)

		select {
		case <-time.After(5 * time.Second):
		case <-child.Done():
			t.Fatal("fail")
		}

		child.Autoclose()

		select {
		case <-time.After(5 * time.Second):
			t.Fatal("fail")
		case <-child.Done():
		}
	})

	t.Run(".Close cancels child contexts", func(t *testing.T) {
		p := process.New("")
		p.Start()

		child := process.New("child")
		p.StartChild(child)

		canceled1 := testutils.NewAwaiter()
		canceled2 := testutils.NewAwaiter()

		chDone1 := p.Go("foo", func(ctx process.Context) {
			select {
			case <-ctx.Closing():
				canceled1.ItHappened()
			case <-time.After(5 * time.Second):
				t.Fatal("context wasn't canceled")
			}
		}).Done()

		chDone2 := child.Go("foo", func(ctx process.Context) {
			select {
			case <-ctx.Closing():
				canceled2.ItHappened()
			case <-time.After(5 * time.Second):
				t.Fatal("context wasn't canceled")
			}
		}).Done()

		requireDone(t, p.Done(), false)
		requireDone(t, child.Done(), false)
		requireDone(t, chDone1, false)
		requireDone(t, chDone2, false)

		go p.Close()

		canceled1.AwaitOrFail(t)
		canceled2.AwaitOrFail(t)

		require.Eventually(t, func() bool { return isDone(t, p.Done()) }, 5*time.Second, 100*time.Millisecond)
		require.Eventually(t, func() bool { return isDone(t, child.Done()) }, 5*time.Second, 100*time.Millisecond)
		require.Eventually(t, func() bool { return isDone(t, chDone1) }, 5*time.Second, 100*time.Millisecond)
		require.Eventually(t, func() bool { return isDone(t, chDone2) }, 5*time.Second, 100*time.Millisecond)
	})
}

func requireDone(t *testing.T, chDone <-chan struct{}, done bool) {
	t.Helper()
	require.Equal(t, done, isDone(t, chDone))
}

func isDone(t *testing.T, chDone <-chan struct{}) bool {
	t.Helper()
	select {
	case <-chDone:
		return true
	default:
		return false
	}
}
