package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestShutdownOrchestratorRunsStepsInLIFOOrder(t *testing.T) {
	orch := newShutdownOrchestrator()
	var order []string
	for _, name := range []string{"first", "second", "third"} {
		name := name
		orch.register(name, func(context.Context) error {
			order = append(order, name)
			return nil
		})
	}

	orch.run()

	want := []string{"third", "second", "first"}
	if len(order) != len(want) {
		t.Fatalf("executed steps = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("executed steps = %v, want %v", order, want)
		}
	}
}

func TestShutdownOrchestratorPanicDoesNotStopSequence(t *testing.T) {
	orch := newShutdownOrchestrator()
	var order []string
	orch.register("last", func(context.Context) error {
		order = append(order, "last")
		return nil
	})
	orch.register("panics", func(context.Context) error {
		panic("boom")
	})
	orch.register("errors", func(context.Context) error {
		order = append(order, "errors")
		return errors.New("close failed")
	})

	orch.run()

	if len(order) != 2 || order[0] != "errors" || order[1] != "last" {
		t.Fatalf("executed steps = %v, want [errors last]", order)
	}
}

func TestShutdownOrchestratorRunIsOnceAndWaits(t *testing.T) {
	orch := newShutdownOrchestrator()
	var mu sync.Mutex
	count := 0
	orch.register("step", func(context.Context) error {
		mu.Lock()
		count++
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			orch.run()
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("step executed %d times, want 1", count)
	}
}

func TestShutdownOrchestratorWatchdogForcesExit(t *testing.T) {
	orch := newShutdownOrchestrator()
	orch.budget = 50 * time.Millisecond
	exitCodes := make(chan int, 1)
	orch.exitFn = func(code int) { exitCodes <- code }

	release := make(chan struct{})
	orch.register("stuck", func(context.Context) error {
		<-release
		return nil
	})

	go orch.run()

	select {
	case code := <-exitCodes:
		if code != watchdogExitCode {
			t.Fatalf("exit code = %d, want %d", code, watchdogExitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not fire")
	}
	close(release)
	orch.run() // drain: wait for the stuck step to finish before test cleanup
}
