package jsvm

import (
	"sync"
	"time"

	"github.com/grafana/sobek"
)

type Timers struct {
	rt             *sobek.Runtime
	eventLoop      *EventLoop
	timerIDCounter uint64
	timers         map[uint64]*time.Timer
	cancels        map[uint64]func()
	mu             sync.Mutex
}

func NewTimers(rt *sobek.Runtime, eventLoop *EventLoop) *Timers {
	return &Timers{
		rt:        rt,
		eventLoop: eventLoop,
		timers:    make(map[uint64]*time.Timer),
		cancels:   make(map[uint64]func()),
	}
}

func (t *Timers) SetupGlobally() error {
	if err := t.rt.Set("setTimeout", t.setTimeout); err != nil {
		return err
	}
	if err := t.rt.Set("clearTimeout", t.clearTimeout); err != nil {
		return err
	}
	if err := t.rt.Set("setInterval", t.setInterval); err != nil {
		return err
	}
	if err := t.rt.Set("clearInterval", t.clearTimeout); err != nil {
		return err
	}
	return nil
}

func (t *Timers) setTimeout(callback sobek.Callable, delay float64) uint64 {
	return t.schedule(callback, delay, false)
}

func (t *Timers) setInterval(callback sobek.Callable, delay float64) uint64 {
	if delay < 0 {
		delay = 0
	}

	duration := time.Duration(delay * float64(time.Millisecond))

	t.mu.Lock()
	t.timerIDCounter++
	id := t.timerIDCounter
	// placeholder to mark active interval before first scheduling
	t.timers[id] = nil
	t.mu.Unlock()

	t.scheduleInterval(id, callback, duration)

	return id
}

func (t *Timers) schedule(callback sobek.Callable, delay float64, repeat bool) uint64 {
	if repeat {
		return t.setInterval(callback, delay)
	}

	t.mu.Lock()
	t.timerIDCounter++
	id := t.timerIDCounter
	t.mu.Unlock()

	if delay < 0 {
		delay = 0
	}
	duration := time.Duration(delay * float64(time.Millisecond))

	enqueueCallback, cancelCallback := t.eventLoop.RegisterCancelableCallback()

	timer := time.AfterFunc(duration, func() {
		enqueueCallback(func() error {
			t.mu.Lock()
			delete(t.cancels, id)
			t.mu.Unlock()

			_, err := callback(sobek.Undefined())
			if err != nil {
				return err
			}

			// Clean up one-shot timer
			t.mu.Lock()
			delete(t.timers, id)
			t.mu.Unlock()
			return nil
		})
	})

	t.mu.Lock()
	t.timers[id] = timer
	t.cancels[id] = cancelCallback
	t.mu.Unlock()

	return id
}

func (t *Timers) scheduleInterval(id uint64, callback sobek.Callable, duration time.Duration) {
	enqueueCallback, cancelCallback := t.eventLoop.RegisterCancelableCallback()

	timer := time.AfterFunc(duration, func() {
		enqueueCallback(func() error {
			t.mu.Lock()
			delete(t.cancels, id)
			_, exists := t.timers[id]
			t.mu.Unlock()

			_, err := callback(sobek.Undefined())
			if err != nil {
				return err
			}

			if exists {
				t.scheduleInterval(id, callback, duration)
			}

			return nil
		})
	})

	t.mu.Lock()
	if _, exists := t.timers[id]; !exists {
		t.mu.Unlock()
		timer.Stop()
		cancelCallback()
		return
	}
	t.timers[id] = timer
	t.cancels[id] = cancelCallback
	t.mu.Unlock()
}

func (t *Timers) clearTimeout(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if timer, exists := t.timers[id]; exists {
		if timer != nil {
			timer.Stop()
		}
		delete(t.timers, id)
	}

	if cancel, exists := t.cancels[id]; exists {
		delete(t.cancels, id)
		cancel()
	}
}
