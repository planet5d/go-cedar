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
		p := process.StartNew("Autoclose")

		spawnN(p, 8, 1*time.Second)

		p.Autoclose()

		select {
		case <-time.After(2 * time.Second):
			t.Fatal("fail")
		case <-p.Done():
		}
	})

	t.Run("it does a thing", func(t *testing.T) {
		p := process.StartNew("Go spawn")

		spawnN(p, 12, 1*time.Second)

		select {
		case <-time.After(2 * time.Second):
		case <-p.Done():
			t.Fatal("fail")
		}

		p.Autoclose()

		select {
		case <-time.After(1 * time.Second):
			t.Fatal("fail")
		case <-p.Done():
		}
	})

	t.Run("child test 1", func(t *testing.T) {
		p := process.StartNew("Autoclose with children")
		defer p.Close()

		child := &process.Process{}
		p.StartChild(child, "child")
		spawnN(child, 10, 1*time.Second)

		child.Autoclose()

		select {
		case <-time.After(2 * time.Second):
			t.Fatal("fail")
		case <-child.Done():
		}
	})

	t.Run("child test 2", func(t *testing.T) {
		p := process.StartNew("child tester")
		defer p.Close()

		child := &process.Process{}
		p.StartChild(child, "child")
		spawnN(child, 20, 1*time.Second)

		select {
		case <-time.After(3 * time.Second):
		case <-child.Done():
			t.Fatal("fail")
		}

		spawnN(child, 20, 1*time.Second)

		child.Autoclose()

		select {
		case <-time.After(3 * time.Second):
			t.Fatal("fail")
		case <-child.Done():
		}
	})

	t.Run(".Close cancels child contexts", func(t *testing.T) {
		p := process.StartNew("close tester")

		child := &process.Process{}
		p.StartChild(child, "child")

		canceled1 := testutils.NewAwaiter()
		canceled2 := testutils.NewAwaiter()

		foo1, _ := p.Go("foo1", func(ctx process.Context) {
			select {
			case <-ctx.Closing():
				canceled1.ItHappened()
			case <-time.After(5 * time.Second):
				t.Fatal("context wasn't canceled")
			}
		})

		foo2, _ := child.Go("foo2", func(ctx process.Context) {
			select {
			case <-ctx.Closing():
				canceled2.ItHappened()
			case <-time.After(5 * time.Second):
				t.Fatal("context wasn't canceled")
			}
		})

		requireDone(t, p.Done(), false)
		requireDone(t, child.Done(), false)
		requireDone(t, foo1.Done(), false)
		requireDone(t, foo2.Done(), false)

		go p.Close()

		canceled1.AwaitOrFail(t)
		canceled2.AwaitOrFail(t)

		require.Eventually(t, func() bool { return isDone(t, p.Done()) }, 5*time.Second, 100*time.Millisecond)
		require.Eventually(t, func() bool { return isDone(t, child.Done()) }, 5*time.Second, 100*time.Millisecond)
		require.Eventually(t, func() bool { return isDone(t, foo1.Done()) }, 5*time.Second, 100*time.Millisecond)
		require.Eventually(t, func() bool { return isDone(t, foo2.Done()) }, 5*time.Second, 100*time.Millisecond)
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
