package esmvm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/grafana/sobek"
)

// EventLoop manages async callback execution for a Sobek runtime.
type EventLoop struct {
	rt                  *sobek.Runtime
	queue               []func() error
	registeredCallbacks int
	lock                sync.Mutex
	wakeupCh            chan struct{}
	ctx                 context.Context
}

// NewEventLoop creates a new event loop for the given runtime.
func NewEventLoop(rt *sobek.Runtime, ctx context.Context) *EventLoop {
	return &EventLoop{
		rt:       rt,
		queue:    make([]func() error, 0, 10),
		wakeupCh: make(chan struct{}, 1),
		ctx:      ctx,
	}
}

// RegisterCallback reserves a callback slot for async work.
// Returns an enqueue function that should be called exactly once with the callback.
func (e *EventLoop) RegisterCallback() (enqueueCallback func(func() error)) {
	enqueueCallback, _ = e.RegisterCancelableCallback()
	return enqueueCallback
}

// RegisterCancelableCallback reserves a callback slot for async work.
// It returns:
//   - enqueue callback: to queue the actual callback (call exactly once)
//   - cancel callback: to release the reserved slot if async work is aborted
func (e *EventLoop) RegisterCancelableCallback() (enqueueCallback func(func() error), cancelCallback func()) {
	e.lock.Lock()
	var callbackCalled bool
	e.registeredCallbacks++
	e.lock.Unlock()

	finalize := func() bool {
		e.lock.Lock()
		if callbackCalled {
			e.lock.Unlock()
			return false
		}
		callbackCalled = true
		e.registeredCallbacks--
		e.lock.Unlock()
		return true
	}

	enqueueCallback = func(f func() error) {
		if !finalize() {
			panic("RegisterCallback enqueue function called twice")
		}

		e.lock.Lock()
		e.queue = append(e.queue, f)
		e.lock.Unlock()
		e.wakeup()
	}

	cancelCallback = func() {
		if finalize() {
			e.wakeup()
		}
	}

	return enqueueCallback, cancelCallback
}

// Start runs the event loop until all callbacks complete.
// Executes the optional firstCallback immediately before starting.
func (e *EventLoop) Start(firstCallback func() error) error {
	if firstCallback != nil {
		e.lock.Lock()
		e.queue = []func() error{firstCallback}
		e.lock.Unlock()
	}

	for {
		queue, awaiting := e.popAll()

		// Execute all queued callbacks
		for i, f := range queue {
			if err := f(); err != nil {
				// Put unexecuted callbacks back
				e.putInfront(queue[i+1:])
				return err
			}
		}

		// If nothing queued but async work pending: wait
		if awaiting {
			select {
			case <-e.wakeupCh:
				continue
			case <-e.ctx.Done():
				return e.ctx.Err()
			}
		}

		// We had work and finished it - re-check pending state before exit.
		if len(queue) > 0 {
			continue
		}

		// Queue empty and no pending work = done
		return nil
	}
}

// WaitOnRegistered blocks until all pending callbacks complete.
func (e *EventLoop) WaitOnRegistered() error {
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		e.lock.Lock()
		awaiting := e.registeredCallbacks > 0 || len(e.queue) > 0
		e.lock.Unlock()

		if !awaiting {
			return nil
		}

		select {
		case <-e.wakeupCh:
			continue
		case <-ticker.C:
			continue
		case <-timeout:
			return fmt.Errorf("event loop timeout: %d callbacks pending", e.registeredCallbacks)
		case <-e.ctx.Done():
			return e.ctx.Err()
		}
	}
}

func (e *EventLoop) wakeup() {
	select {
	case e.wakeupCh <- struct{}{}:
	default:
	}
}

func (e *EventLoop) popAll() (queue []func() error, awaitingCallbacks bool) {
	e.lock.Lock()
	defer e.lock.Unlock()
	queue = e.queue
	e.queue = make([]func() error, 0, 10)
	awaitingCallbacks = e.registeredCallbacks > 0
	return queue, awaitingCallbacks
}

func (e *EventLoop) putInfront(tasks []func() error) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.queue = append(tasks, e.queue...)
}
