package process

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/planet5d/go-cedar/log"
	"github.com/planet5d/go-cedar/utils"
)

var _ Context = (*Process)(nil)

// Errors
var (
	ErrUnstarted = errors.New("unstarted")
	ErrClosed    = errors.New("closed")
)

var gSpawnCounter = int64(0)

func nextProcessName(baseName string) (string, int64) {
	pid := atomic.AddInt64(&gSpawnCounter, 1)
	return baseName + " #" + strconv.FormatInt(pid, 10), pid
}

func (p *Process) ProcessInit(name string) {
	*p = Process{}
	p.name = name
	p.state = Unstarted
}

func (p *Process) OnClosing() {
	// Intended for client override
}

func (p *Process) OnClosed() {
	// Intended for client override
}

func (p *Process) Close() {
	p.closeOnce.Do(func() {
		if p.state != Started {
			panic("not started")
		}

		// Signal that a Close() has been ordered, causing all children receive a Close()
		p.state = Closing
		close(p.chClosing)

		// Callback while we wait for children
		p.OnClosing()

		// Wait for all children to close, then we proceed with completion.
		p.exec.Wait()
		p.state = Closed
		p.OnClosed()
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
		p.exec.Wait()

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

func (p *Process) ProcessID() int64 {
	return p.id
}

func (p *Process) ProcessName() string {
	return p.name
}

func (p *Process) Start() error {
	if p.state != Unstarted {
		panic("already started")
	}
	p.name, p.id = nextProcessName(p.name)
	p.chClosing = make(chan struct{})
	p.chClosed = make(chan struct{})
	p.Logger = log.NewLogger(p.name)
	p.state = Started
	return nil
}

// Writes pretty debug state info of a given verbosity level.
// If out == nil, the text output is instead directed to this context's logger.Info()
func (p *Process) PrintProcessTree(out io.Writer, verboseLevel int32) {
	tree := p.ExportProcessTree()
	txt := utils.PrettyJSON(tree)

	if out != nil {
		out.Write([]byte(txt))
	} else {
		p.Info(verboseLevel, txt)
	}
}

func (p *Process) ExportProcessTree() map[string]interface{} {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()

	treeNode := make(map[string]interface{}, 3)
	treeNode["id"] = p.id
	treeNode["status"] = p.state.String()
	if len(p.subs) > 0 {
		children := make(map[string]interface{}, len(p.subs))
		treeNode["children"] = children
		for child := range p.subs {
			children[child.ProcessName()] = child.ExportProcessTree()
		}
	}

	return treeNode
}

func (p *Process) ChildCount() int {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()
	return len(p.subs)
}

// StartChild() starts the given child (in the current thread) and then adds it as a child process.
func (p *Process) StartChild(child Context) error {
	if p.state != Started {
		return ErrUnstarted
	}

	err := child.Start()
	if err != nil {
		return err
	}

	p.subsMu.Lock()
	if p.subs == nil {
		p.subs = make(map[Context]struct{})
	}
	p.subs[child] = struct{}{}
	p.subsMu.Unlock()

	p.exec.Add(1)
	go func() {
		select {
		case <-p.Closing():
			child.Close()
		case <-child.Done():
		}

		p.exec.Done()
		p.subsMu.Lock()
		delete(p.subs, child)
		p.subsMu.Unlock()
	}()

	return nil
}

func (p *Process) Go(name string, fn func(ctx Context)) Context {
	child := &Process{}
	child.ProcessInit(name)

	err := p.StartChild(child)
	if err != nil {
		panic(err)
	}

	go func() {
		fn(child)
		child.Close()
	}()

	return child
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
