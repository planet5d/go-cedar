package process

import (
	"context"
	"sync"

	"github.com/planet5d/go-cedar/log"
)

func New(name string) *Process {
	p := &Process{}
	p.ProcessInit(name)
	return p
}

func Init(p *Process, name string) {
	p.ProcessInit(name)
}

// Process is typically embedded in a struct that contains fields associated with a given process.
type Process struct {
	log.Logger

	id        int64
	state     State
	name      string
	closeOnce sync.Once
	chClosing chan struct{}  // signals Close() has been called and close execution has begun.
	chClosed  chan struct{}  // signals Close() has been called and all close execution is done.
	err       error          // See context.Err() for spec
	running   sync.WaitGroup // blocks until all execution is complete
	subsMu    sync.Mutex     // Locked when .subs is being accessed
	subs      []Context
}

// A Process implements all aspects of a process.Context
var NilContext = Context((*Process)(nil))

type Context interface {
	log.Logger

	// A process.Context can be used just like a context.Context, cowgirl.
	context.Context

	// A guaranteed unique name derived from the given name.
	ProcessName() string

	// A guaranteed unique ID assigned after Start() is called.
	ProcessID() int64

	// Returns a tree reflecting the current state for debugging and diagnostics.
	ExportProcessTree() map[string]interface{}

	// Starts this process context.
	// Panics if this process has already been started.
	Start() error

	// Returns the number of children currently running (that are started or are closing),
	// Since this count can change at any time, the caller must take precautions as needed.
	ChildCount() int

	// Calls child.Start() and then adds the given child to this Process (if no error).
	// If child.Start() returns an error, then child.Close() is called and the error is returned.
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
