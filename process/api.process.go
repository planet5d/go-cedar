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

	// A process.Context can be used just like a context.Context.
	context.Context

	// OnContextInit is called when this context is to be overwritten / reset typically called by StartChild().
	// This is called before this Context is running and intended to initialize state.
	OnContextInit(label string) error

	// A guaranteed unique label derived from the given label.
	ContextLabel() string

	// A guaranteed unique ID assigned after Start() is called.
	ContextID() int64

	// Calls child.OnContextInit(), adds it as a child to this Context, and then calls child.OnContextStarted().
	// If an error is encountered, then child.Close() is immediately called and the error is returned.
	// Context implementations wishing to remain lightweight just use:
	//    if err = child.OnContextInit(label); err != nil {
	//        return err
	//    }
	//    if err = child.OnContextStarted(); err != nil {
	//        return err
	//    }
	StartChild(child Context, label string) error

	// OnContextStarted is a callback once this Context is started, called at the end of StartChild().
	// This method is indended for override and is where a Context typically starts child service Contexts.
	// If an error is returned, Context.Close() is immediately called and the error is returned from StartChild().
	OnContextStarted() error
	
	// Returns a tree reflecting the current state for debugging and diagnostics.
	ExportContextTree() map[string]interface{}

	// Appends all currently open/active child Contexts to the given slice and returns the given slice.
	// Naturally, the returned items are back-ward looking as any could close at any time.
	// Context implementations wishing to remain lightweight may opt to not retain a list of children (and just return the given slice as-is).
	GetChildren(in []Context) []Context 

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
