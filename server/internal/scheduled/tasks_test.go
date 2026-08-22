package scheduled

import (
	"context"
	"testing"
	"time"
)

type instantSchedule struct{}

func (instantSchedule) Next(t time.Time) time.Time { return t.Add(5 * time.Millisecond) }

// Stop must wait for a job that is mid-flight instead of abandoning it.
func TestStopWaitsForInFlightJob(t *testing.T) {
	svc := NewService(nil, nil)
	started := make(chan struct{})
	finished := make(chan struct{})
	svc.cron.Schedule(instantSchedule{}, jobFunc(func() {
		close(started)
		time.Sleep(300 * time.Millisecond)
		close(finished)
	}))
	svc.cron.Start()
	<-started

	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Stop returned before the in-flight job finished")
	}
}

// A caller deadline shorter than the five-second cap wins, so shutdown never
// hangs on a stuck job.
func TestStopGivesUpOnStuckJob(t *testing.T) {
	svc := NewService(nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	svc.cron.Schedule(instantSchedule{}, jobFunc(func() {
		close(started)
		<-release
	}))
	svc.cron.Start()
	<-started
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := svc.Stop(ctx); err == nil {
		t.Fatal("Stop should report the abandoned job")
	}
}

type jobFunc func()

func (f jobFunc) Run() { f() }
