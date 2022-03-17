package process

import (
	"context"
	"sync"

	"github.com/planet5d/go-cedar/log"
)

// Process is typically embedded in a struct that contains fields associated with a given process.
type Process struct {
	log.Logger

	// Callback after Close() is first called and children are signaled to close.
	// NOTE: This is called from within Close() and should never be invoked explicitly.
	OnClosing func()

	// Callback after Close() and all children have completed Close() (but immediately before Done() is released)
	// NOTE: This is called from within Close() and should never be invoked explicitly.
	OnClosed func()

	id        int64
	state     State
	label     string
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

// StartNew starts a new process Context was its own root (no parent)
func StartNew(label string) Context {
	p := &Process{}
	NilContext.StartChild(p, label)
	return p
}

// Start starts the given context as its own process root.
func Start(child Context, label string) error {
	return NilContext.StartChild(child, label)
}

type Context interface {
	log.Logger

	// A process.Context can be used just like a context.Context, cowgirl.
	context.Context

	// ProcessReset is called when this context is to be overwritten / reset
	ProcessReset(label string)

	// A guaranteed unique label derived from the given label.
	ProcessLabel() string

	// A guaranteed unique ID assigned after Start() is called.
	ProcessID() int64

	// Returns a tree reflecting the current state for debugging and diagnostics.
	ExportProcessTree() map[string]interface{}

	// Returns the number of children currently running (that are started or are closing),
	// Since this count can change at any time, the caller must take precautions as needed.
	ChildCount() int

	// Calls child.ProcessReset() then adds the given Context as a child to this Process Context (assuming no error).
	// If child.OnStart() returns an error, then child.Close() is called and the error is also returned.
	// If parent == nil, this context is started with no parent (typical for "root" Contexts)
	StartChild(child Context, label string) error

	// Callback when this Context is started, called during StartChild().
	// This is indended for override and is implemented by default as a stub.
	// If an error is returned, Context.Close() is immediately called and the error is returned from StartChild().
	OnStart() error

	// Convenience function for StartChild() that makes a new Process wrapper around the given fn and starts it.
	// This newly started child Process is passed to the fn as well as returned for arbitrary access.
	//
	// If fn == nil, this is equivalent to:
	//      child := new(Process).InitProcess(label)
	//      parent.StartChild(child)
	//      child.Autoclose()
	Go(label string, fn func(ctx Context)) (Context, error)

	// Initiates process shutdown and causes all childen's Close() to be called.
	// Close can be called multiple times asynchronously (thanks to an internal sync.Once guard)
	// First, child processes get Close() if applicable.
	// After all children are done closing, OnClosing(), then OnClosed() are executed.
	Close()

	// Automatically calls Close() when this Process has no more child processes.
	// Warning: this can only be used when a ptr to the process is NOT retained.  Otherwise, there would be an unavoidable race condition between close detection and updating the ptr.
	Autoclose()

	// Signals when Close() has been called.
	// First, Child processes get Close(),  then OnClosing, then OnClosed are executing
	Closing() <-chan struct{}

	// Signals when Close() has fully executed, no children remain, and OnClosed() has been completed.
	Done() <-chan struct{}
}
