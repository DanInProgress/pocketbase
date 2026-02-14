package jsvm

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/sobek"
)

func TestEventLoopBasic(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())

	executed := false
	err := loop.Start(func() error {
		executed = true
		return nil
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !executed {
		t.Fatal("Expected callback to be executed")
	}
}

func TestEventLoopRegisterCallback(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())

	var results []int
	enqueue := loop.RegisterCallback()

	go func() {
		time.Sleep(10 * time.Millisecond)
		enqueue(func() error {
			results = append(results, 42)
			return nil
		})
	}()

	err := loop.Start(nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(results) != 1 || results[0] != 42 {
		t.Fatalf("Expected results [42], got: %v", results)
	}
}

func TestEventLoopMultipleCallbacks(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())

	var results []int

	enqueue1 := loop.RegisterCallback()
	enqueue2 := loop.RegisterCallback()
	enqueue3 := loop.RegisterCallback()

	go func() {
		time.Sleep(5 * time.Millisecond)
		enqueue1(func() error {
			results = append(results, 1)
			return nil
		})
	}()

	go func() {
		time.Sleep(10 * time.Millisecond)
		enqueue2(func() error {
			results = append(results, 2)
			return nil
		})
	}()

	go func() {
		time.Sleep(15 * time.Millisecond)
		enqueue3(func() error {
			results = append(results, 3)
			return nil
		})
	}()

	err := loop.Start(nil)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got: %d", len(results))
	}
}

func TestTimersSetTimeout(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	timers := NewTimers(vm, loop)
	if err := timers.SetupGlobally(); err != nil {
		t.Fatalf("Failed to setup timers: %v", err)
	}

	var executed bool
	vm.Set("callback", func() { executed = true })

	err := loop.Start(func() error {
		_, err := vm.RunString("setTimeout(callback, 10)")
		return err
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !executed {
		t.Fatal("setTimeout callback not executed")
	}
}

func TestTimersSetTimeoutMultiple(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	timers := NewTimers(vm, loop)
	if err := timers.SetupGlobally(); err != nil {
		t.Fatalf("Failed to setup timers: %v", err)
	}

	var results []int
	vm.Set("callback1", func() { results = append(results, 1) })
	vm.Set("callback2", func() { results = append(results, 2) })
	vm.Set("callback3", func() { results = append(results, 3) })

	err := loop.Start(func() error {
		_, err := vm.RunString(`
			setTimeout(callback1, 5);
			setTimeout(callback2, 10);
			setTimeout(callback3, 15);
		`)
		return err
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Expected 3 callbacks executed, got: %d", len(results))
	}
}

func TestTimersSetInterval(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	timers := NewTimers(vm, loop)
	if err := timers.SetupGlobally(); err != nil {
		t.Fatalf("Failed to setup timers: %v", err)
	}

	var count int
	vm.Set("callback", func() {
		count++
		if count >= 3 {
			vm.RunString("clearInterval(intervalId)")
		}
	})

	err := loop.Start(func() error {
		_, err := vm.RunString("var intervalId = setInterval(callback, 10)")
		return err
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if count != 3 {
		t.Fatalf("Expected interval to fire 3 times, got: %d", count)
	}
}

func TestTimersClearTimeout(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	timers := NewTimers(vm, loop)
	if err := timers.SetupGlobally(); err != nil {
		t.Fatalf("Failed to setup timers: %v", err)
	}

	var executed bool
	vm.Set("callback", func() { executed = true })

	err := loop.Start(func() error {
		_, err := vm.RunString(`
			var timeoutId = setTimeout(callback, 100);
			clearTimeout(timeoutId);
		`)
		return err
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if executed {
		t.Fatal("Callback should not have been executed after clearTimeout")
	}
}

func TestEventLoopErrorHandling(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())

	err := loop.Start(func() error {
		_, err := vm.RunString("throw new Error('test error')")
		return err
	})

	if err == nil {
		t.Fatal("Expected error from thrown exception, got nil")
	}
}

func TestEventLoopWaitOnRegistered(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	timers := NewTimers(vm, loop)
	if err := timers.SetupGlobally(); err != nil {
		t.Fatalf("Failed to setup timers: %v", err)
	}

	var executed bool
	vm.Set("callback", func() { executed = true })

	go func() {
		loop.Start(func() error {
			_, err := vm.RunString("setTimeout(callback, 50)")
			return err
		})
	}()

	// Give loop time to start
	time.Sleep(10 * time.Millisecond)

	err := loop.WaitOnRegistered()
	if err != nil {
		t.Fatalf("WaitOnRegistered failed: %v", err)
	}
	if !executed {
		t.Fatal("Callback should have executed before WaitOnRegistered returned")
	}
}

func TestTimersZeroDelay(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	timers := NewTimers(vm, loop)
	if err := timers.SetupGlobally(); err != nil {
		t.Fatalf("Failed to setup timers: %v", err)
	}

	var executed bool
	vm.Set("callback", func() { executed = true })

	err := loop.Start(func() error {
		_, err := vm.RunString("setTimeout(callback, 0)")
		return err
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !executed {
		t.Fatal("setTimeout with 0 delay should execute callback")
	}
}

func TestTimersNegativeDelay(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	timers := NewTimers(vm, loop)
	if err := timers.SetupGlobally(); err != nil {
		t.Fatalf("Failed to setup timers: %v", err)
	}

	var executed bool
	vm.Set("callback", func() { executed = true })

	err := loop.Start(func() error {
		_, err := vm.RunString("setTimeout(callback, -100)")
		return err
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !executed {
		t.Fatal("setTimeout with negative delay should execute callback immediately")
	}
}
