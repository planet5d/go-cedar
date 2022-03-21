package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/planet5d/go-cedar/log"
	"github.com/planet5d/go-cedar/utils"
)

var _ Context = (*Process)(nil)

// Errors
var (
	ErrAlreadyStarted = errors.New("already started")
	ErrUnstarted      = errors.New("unstarted")
	ErrClosed         = errors.New("closed")
)

var gSpawnCounter = int64(0)

func nextProcessLabel(baseLabel string) (string, int64) {
	pid := atomic.AddInt64(&gSpawnCounter, 1)
	return fmt.Sprint(baseLabel, " #", pid), pid
}

func (p *Process) OnContextInit(label string) error {
	*p = Process{}
	p.state = Unstarted
	p.label, p.id = nextProcessLabel(label)
	p.chClosing = make(chan struct{})
	p.chClosed = make(chan struct{})
	p.Logger = log.NewLogger(p.label)
	p.state = Started
	return nil
}

func (p *Process) OnContextStarted() error {
	return nil // Intended for client override
}

func (p *Process) Close() {
	p.closeOnce.Do(func() {

		// Signal that a Close() has been ordered, causing all children receive a Close()
		p.state = Closing
		close(p.chClosing)

		// Fire callback if given
		if p.OnClosing != nil {
			p.OnClosing()
			p.OnClosing = nil
		}

		// Wait for all children to close, then we proceed with completion.
		p.running.Wait()
		p.state = Closed

		// Fire callback if given
		if p.OnClosed != nil {
			p.OnClosed()
			p.OnClosed = nil
		}

		close(p.chClosed)
	})
}

func (p *Process) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}

func (p *Process) Err() error {
	select {
	case <-p.Done():
		if p.err == nil {
			return context.Canceled
		}
		return p.err
	default:
		return nil
	}
}

func (p *Process) Value(key interface{}) interface{} {
	return nil
}

func (p *Process) Autoclose() {
	go func() {
		p.running.Wait()

		/*
			prevSeed := int64(-1)
			for checkSubs := true; checkSubs; {
				p.wg.Wait()

				// If no delay given, proceed to immediately Close.
				if delay <= 0 {
					break
				}

				ticker := time.NewTicker(delay)
				select {
				case <-ticker.C:
					// The first time around, checkSubs is guaranteed to be true
					curSeed := p.seed
					checkSubs = p.seed != prevSeed
					prevSeed = curSeed
				case <-p.Closing():
					checkSubs = false
				}
				ticker.Stop()
			}
		*/

		p.Close()
	}()
}

func (p *Process) ContextID() int64 {
	return p.id
}

func (p *Process) ContextLabel() string {
	return p.label
}

// Writes pretty debug state info of a given verbosity level.
// If out == nil, the text output is instead directed to this context's logger.Info()
func (p *Process) PrintContextTree(out io.Writer, verboseLevel int32) {
	tree := p.ExportContextTree()
	txt := utils.PrettyJSON(tree)

	if out != nil {
		out.Write([]byte(txt))
	} else {
		p.Info(verboseLevel, txt)
	}
}

func (p *Process) ExportContextTree() map[string]interface{} {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()

	treeNode := make(map[string]interface{}, 3)
	treeNode["id"] = p.id
	treeNode["status"] = p.state.String()
	if len(p.subs) > 0 {
		children := make(map[string]interface{}, len(p.subs))
		treeNode["children"] = children
		for _, child := range p.subs {
			children[child.ContextLabel()] = child.ExportContextTree()
		}
	}

	return treeNode
}

func (p *Process) GetChildren(in []Context) []Context {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()
	return append(in, p.subs...)
}

func (p *Process) ChildCount() int {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()
	return len(p.subs)
}

// StartChild starts the given child Context as a "sub" processs.
func (p *Process) StartChild(child Context, label string) error {
	if err := child.OnContextInit(label); err != nil {
		child.Close()
		return err
	}

	if p != nil {
		if p.state != Started {
			return ErrUnstarted
		}

		// add new child to parent
		p.subsMu.Lock()
		p.subs = append(p.subs, child)
		p.subsMu.Unlock()

		p.running.Add(1)
		go func() {

			// block until parent is closing or child has completed closing
			select {
			case <-p.Closing():
				child.Close()
			case <-child.Closing():
			}

			<-child.Done()

			// update the running count and remove the sub
			p.running.Done()
			p.subsMu.Lock()
			{
				N := len(p.subs)
				for i := 0; i < N; i++ {
					if p.subs[i] == child {
						copy(p.subs[i:], p.subs[i+1:N])
						N--
						p.subs[N] = nil // show GC some love
						p.subs = p.subs[:N]
					}
				}
			}
			p.subsMu.Unlock()
		}()
	}

	err := child.OnContextStarted()
	if err != nil {
		child.Close()
		return err
	}

	return nil
}

func (p *Process) Go(label string, fn func(ctx Context)) (Context, error) {
	child := &Process{}

	err := p.StartChild(child, label)
	if err != nil {
		return nil, err
	}

	go func() {
		fn(child)
		child.Close()
	}()

	return child, nil
}

func (p *Process) Closing() <-chan struct{} {
	return p.chClosing
}

func (p *Process) Done() <-chan struct{} {
	return p.chClosed
}

type State int32

const (
	Unstarted State = iota
	Started
	Closing
	Closed
)

func (s State) String() string {
	switch s {
	case Unstarted:
		return "unstarted"
	case Started:
		return "started"
	case Closing:
		return "closing"
	case Closed:
		return "closed"
	default:
		return "(err: unknown)"
	}
}
