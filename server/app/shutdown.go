package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

const (
	// shutdownBudget bounds the whole close sequence; "stops within 10s" is
	// the R-1.1 acceptance criterion.
	shutdownBudget = 10 * time.Second
	// watchdogExitCode distinguishes a forced exit from a clean shutdown.
	watchdogExitCode = 3
)

type shutdownStep struct {
	name string
	fn   func(context.Context) error
}

// shutdownOrchestrator collects close actions as resources are created in
// Start and runs them in reverse (LIFO) order on shutdown, so dependents shut
// down before their dependencies. A watchdog force-exits the process if the
// sequence exceeds its budget — "stops eventually" must never depend on every
// step behaving.
type shutdownOrchestrator struct {
	budget time.Duration
	exitFn func(int)

	mu      sync.Mutex
	steps   []shutdownStep
	current string

	once sync.Once
	done chan struct{}
}

func newShutdownOrchestrator() *shutdownOrchestrator {
	return &shutdownOrchestrator{
		budget: shutdownBudget,
		exitFn: os.Exit,
		done:   make(chan struct{}),
	}
}

// register adds a close action. Call in resource-creation order; run executes
// in reverse.
func (o *shutdownOrchestrator) register(name string, fn func(context.Context) error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.steps = append(o.steps, shutdownStep{name: name, fn: fn})
}

// run executes the close sequence exactly once; concurrent and repeated calls
// wait for that one execution to finish.
func (o *shutdownOrchestrator) run() {
	o.once.Do(func() {
		defer close(o.done)
		o.execute()
	})
	<-o.done
}

func (o *shutdownOrchestrator) execute() {
	ctx, cancel := context.WithTimeout(context.Background(), o.budget)
	defer cancel()

	watchdogDone := make(chan struct{})
	defer close(watchdogDone)
	go func() {
		select {
		case <-watchdogDone:
		case <-time.After(o.budget):
			o.mu.Lock()
			stuck := o.current
			o.mu.Unlock()
			log.Printf("[shutdown] watchdog.timeout step=%s budget=%s forcing exit", stuck, o.budget)
			o.exitFn(watchdogExitCode)
		}
	}()

	o.mu.Lock()
	steps := make([]shutdownStep, len(o.steps))
	copy(steps, o.steps)
	o.mu.Unlock()

	log.Printf("[shutdown] begin steps=%d budget=%s", len(steps), o.budget)
	started := time.Now()
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		o.mu.Lock()
		o.current = step.name
		o.mu.Unlock()
		stepStarted := time.Now()
		if err := runShutdownStep(ctx, step); err != nil {
			log.Printf("[shutdown] step=%s err=%v elapsed=%s", step.name, err, time.Since(stepStarted).Round(time.Millisecond))
		} else {
			log.Printf("[shutdown] step=%s ok elapsed=%s", step.name, time.Since(stepStarted).Round(time.Millisecond))
		}
	}
	log.Printf("[shutdown] done elapsed=%s", time.Since(started).Round(time.Millisecond))
}

// runShutdownStep isolates panics so one broken closer cannot stop the rest
// of the sequence.
func runShutdownStep(ctx context.Context, step shutdownStep) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return step.fn(ctx)
}
