package esmvm

import (
	"sync"

	"github.com/grafana/sobek"
)

type poolItem struct {
	mux       sync.Mutex
	busy      bool
	vm        *sobek.Runtime
	eventLoop *EventLoop
}

type vmsPool struct {
	mux     sync.RWMutex
	factory func() (*sobek.Runtime, *EventLoop)
	items   []*poolItem
}

// newPool creates a new pool with pre-warmed vms generated from the specified factory.
func newPool(size int, factory func() (*sobek.Runtime, *EventLoop)) *vmsPool {
	pool := &vmsPool{
		factory: factory,
		items:   make([]*poolItem, size),
	}

	for i := 0; i < size; i++ {
		vm, eventLoop := pool.factory()
		pool.items[i] = &poolItem{
			vm:        vm,
			eventLoop: eventLoop,
		}
	}

	return pool
}

// run executes "call" with a vm created from the pool
// (either from the buffer or a new one if all buffered vms are busy)
func (p *vmsPool) run(call func(vm *sobek.Runtime) error) error {
	p.mux.RLock()

	// try to find a free item
	var freeItem *poolItem
	for _, item := range p.items {
		item.mux.Lock()
		if item.busy {
			item.mux.Unlock()
			continue
		}
		item.busy = true
		item.mux.Unlock()
		freeItem = item
		break
	}

	p.mux.RUnlock()

	// create a new one-off item if of all of the pool items are currently busy
	//
	// note: if turned out not efficient we may change this in the future
	// by adding the created item in the pool with some timer for removal
	if freeItem == nil {
		vm, eventLoop := p.factory()
		err := eventLoop.Start(func() error {
			return call(vm)
		})
		return err
	}

	// Execute with event loop
	var execErr error
	loopErr := freeItem.eventLoop.Start(func() error {
		execErr = call(freeItem.vm)
		return execErr
	})

	// Wait for event loop to drain
	if drainErr := freeItem.eventLoop.WaitOnRegistered(); drainErr != nil {
		freeItem.mux.Lock()
		freeItem.busy = false
		freeItem.mux.Unlock()
		return drainErr
	}

	// "free" the vm
	freeItem.mux.Lock()
	freeItem.busy = false
	freeItem.mux.Unlock()

	if loopErr != nil {
		return loopErr
	}
	return execErr
}
