package process

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/planet5d/go-cedar/log"
	"github.com/planet5d/go-cedar/utils"
)

type Process struct {
	log.Logger

	id        int64
	state     State
	name      string
	closeOnce sync.Once
	chClosing chan struct{} // signals Close() has been called and close execution has begun.
	chClosed  chan struct{} // signals Close() has been called and all close execution is done.
	err       error         // See context.Err() for spec

	mu       sync.Mutex
	children map[Context]struct{}
	wg       sync.WaitGroup // blocks until child count is 0

}

var NilContext = context.Context(nil)

var _ Context = (*Process)(nil)

type Context interface {

	// A process.Context can be used just like a context.Context, cowgirl.
	context.Context

	// A guaranteed unique name derived from the given name.
	Name() string

	// A guaranteed unique ID assigned after Start() is called.
	ID() int64

	// Returns a tree reflecting the current state for debugging and diagnostics.
	ExportProcessTree() map[string]interface{}

	// Starts this process context.
	// Panics if this process has already been started.
	Start() error

	// Returns the number of children currently running (that are started or are closing),
	// Since this count can change at any time, the caller must take precautions as needed.
	ChildCount() int

	// Calls Start() and then adds the given child to this Process.
	// startFn is an optional fcn that
	StartChild(child Context) error

	// Convenience function for StartChild() that makes a new Process wrapper around the given fn and starts it.
	// This newly started child Process is passed to the fn as well as returned for arbitrary access.
	//
	// If fn == nil, this is equivalent to:
	//      child := new(Process).InitProcess(name)
	//      p.StartChild(child)
	//      child.Autoclose()
	Go(name string, fn func(ctx Context)) Context

	// Initiates process shutdown and causes all childen's Close() to be called.
	// Close can be called multiple times asynchronously (thanks to an internal sync.Once guard)
	// First, child processes get Close() if applicable.
	// After all children are done closing, OnClosing(), then OnClosed() are executed.
	Close()

	// Automatically calls Close() when this Process has no more child processes.
	// Warning: this can only be used when a ptr to the process is NOT retained.  Otherwise, there would be an unavoidable race condition between close detection and updating the ptr.
	Autoclose()

	// Callback when Close() is first called and when children are signaled to exit.
	// NOTE: This is always called from within Close() and should never be called explicitly.
	OnClosing()

	// Callback when during Close() after all children have completed Close() (but immediately before Done() is released)
	// NOTE: This is always called from within Close() and should never be called explicitly.
	OnClosed()

	// Signals when Close() has been called.
	// First, Child processes get Close(),  then OnClosing, then OnClosed are executing
	Closing() <-chan struct{}

	// Signals when Close() has fully executed, no children remain, and OnClosed() has been completed.
	Done() <-chan struct{}
}

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

func New(name string) *Process {
	p := &Process{}
	p.ProcessInit(name)
	return p
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
		p.wg.Wait()
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
		p.wg.Wait()

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

func (p *Process) ID() int64 {
	return p.id
}

func (p *Process) Name() string {
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
	p.mu.Lock()
	defer p.mu.Unlock()

	treeNode := make(map[string]interface{}, 3)
	treeNode["id"] = p.id
	treeNode["status"] = p.state.String()
	if len(p.children) > 0 {
		children := make(map[string]interface{}, len(p.children))
		treeNode["children"] = children
		for child := range p.children {
			children[child.Name()] = child.ExportProcessTree()
		}
	}

	return treeNode
}

func (p *Process) ChildCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.children)
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

	p.mu.Lock()
	if p.children == nil {
		p.children = make(map[Context]struct{})
	}
	p.children[child] = struct{}{}
	p.mu.Unlock()

	p.wg.Add(1)
	go func() {
		select {
		case <-p.Closing():
			child.Close()
		case <-child.Done():
		}

		p.wg.Done()
		p.mu.Lock()
		delete(p.children, child)
		p.mu.Unlock()
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
