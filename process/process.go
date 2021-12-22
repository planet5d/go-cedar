package process

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/planet5d/go-cedar/log"
	"github.com/planet5d/go-cedar/utils"
)

type Process struct {
	log.Logger

	id       int64
	name     string
	mu       sync.RWMutex
	children map[Spawnable]struct{}
	state    State

	goroutines map[*goroutine]struct{}

	closeOnce sync.Once
	chStop    chan struct{}
	chDone    chan struct{}
	wg        sync.WaitGroup
}

var _ Interface = (*Process)(nil)

type Interface interface {
	InitProcess(name string)
	Spawnable
	ProcessTreer
	ID() int64
	Autoclose()
	AutocloseWithCleanup(closeFn func())
	Ctx() context.Context
	NewChild(ctx context.Context, name string) *Process
	SpawnChild(ctx context.Context, child Spawnable) error
	Go(ctx context.Context, name string, fn func(ctx context.Context)) <-chan struct{}
}

type Spawnable interface {
	Name() string
	Start() error
	Close() error
	Done() <-chan struct{}
}

type ProcessTreer interface {
	Spawnable
	ProcessTree() map[string]interface{}
}

var _ Spawnable = (*Process)(nil)
var gSpawnCounter = int64(0)

func nextSwawnName(baseName string) (string, int64) {
	pid := atomic.AddInt64(&gSpawnCounter, 1)
	return baseName + " #" + strconv.FormatInt(pid, 10), pid
}

func (p *Process) InitProcess(name string) {
	p.name, p.id = nextSwawnName(name)
	p.children = make(map[Spawnable]struct{})
	p.goroutines = make(map[*goroutine]struct{})
	p.chStop = make(chan struct{})
	p.chDone = make(chan struct{})
	p.Logger = log.NewLogger(p.name)
}

func New(name string) *Process {
	p := &Process{}
	p.InitProcess(name)
	return p
}

func (p *Process) ID() int64 {
	return p.id
}

func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != Unstarted {
		panic("already started")
	}
	p.state = Started

	return nil
}

func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		func() {
			p.mu.Lock()
			defer p.mu.Unlock()

			if p.state != Started {
				panic("not started")
			}
			p.state = Closed
		}()

		close(p.chStop)
		p.wg.Wait()
		close(p.chDone)
	})
	return nil
}

func (p *Process) Autoclose() {
	go func() {
		p.wg.Wait()
		p.Close()
	}()
}

func (p *Process) AutocloseWithCleanup(closeFn func()) {
	go func() {
		p.wg.Wait()
		closeFn()
		p.Close()
	}()
}

func (p *Process) Name() string {
	return p.name
}

func (p *Process) Ctx() context.Context {
	return utils.ChanContext(p.chStop)
}

func (p *Process) ProcessTree() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var goroutines []string
	for goroutine := range p.goroutines {
		goroutines = append(goroutines, goroutine.name)
	}

	children := make(map[string]interface{}, len(p.children))
	for child := range p.children {
		children[child.Name()], _ = child.(ProcessTreer)
	}
	return map[string]interface{}{
		"status":     p.state.String(),
		"goroutines": goroutines,
		"children":   children,
	}
}

func (p *Process) NewChild(ctx context.Context, name string) *Process {
	child := New(name)
	_ = p.SpawnChild(ctx, child)
	return child
}

var (
	ErrUnstarted = errors.New("unstarted")
	ErrClosed    = errors.New("closed")
)

func (p *Process) SpawnChild(ctx context.Context, child Spawnable) error {
	if p.state != Started {
		return ErrUnstarted
	}

	err := child.Start()
	if err != nil {
		return err
	}
	
	p.mu.Lock()
	p.children[child] = struct{}{}
	p.mu.Unlock()
	
	p.wg.Add(1)
	go func() {
		// If given a Context, use that as a stop signal too
		var ctxDone <-chan struct{}
		if ctx != nil {
			ctxDone = ctx.Done()
		}

		select {
		case <-p.chStop:
			child.Close()
		case <-ctxDone:
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

type goroutine struct {
	name   string
	chDone chan struct{}
}

func (p *Process) Go(ctx context.Context, name string, fn func(ctx context.Context)) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != Started {
		ch := make(chan struct{})
		close(ch)
		return ch
	}

	g := &goroutine{
		chDone: make(chan struct{}),
	}
	g.name, _ = nextSwawnName(name)
	p.goroutines[g] = struct{}{}

	p.wg.Add(1)
	go func() {
		defer func() {
			p.wg.Done()
			p.mu.Lock()
			defer p.mu.Unlock()
			delete(p.goroutines, g)
			close(g.chDone)
		}()

		ctx, cancel := utils.CombinedContext(ctx, p.chStop)
		defer cancel()

		fn(ctx)
	}()

	return g.chDone
}

func (p *Process) Done() <-chan struct{} {
	return p.chDone
}

type State int

const (
	Unstarted State = iota
	Started
	Closed
)

func (s State) String() string {
	switch s {
	case Unstarted:
		return "unstarted"
	case Started:
		return "started"
	case Closed:
		return "closed"
	default:
		return "(err: unknown)"
	}
}
