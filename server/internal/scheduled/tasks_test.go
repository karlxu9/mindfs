package scheduled

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mindfs/server/internal/agent"
	agenttypes "mindfs/server/internal/agent/types"
	"mindfs/server/internal/api/usecase"
	"mindfs/server/internal/fs"
	"mindfs/server/internal/preferences"
	"mindfs/server/internal/session"
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

type fakeRegistry struct {
	root    fs.RootInfo
	manager *session.Manager
}

func (f *fakeRegistry) GetRoot(string) (fs.RootInfo, error)               { return f.root, nil }
func (f *fakeRegistry) GetSessionManager(string) (*session.Manager, error) { return f.manager, nil }
func (f *fakeRegistry) UpsertRoot(string) (fs.RootInfo, error)            { return fs.RootInfo{}, errors.New("not implemented") }
func (f *fakeRegistry) RemoveRoot(string) (fs.RootInfo, error)            { return fs.RootInfo{}, errors.New("not implemented") }
func (f *fakeRegistry) RenameRoot(string, string, string) (fs.RootInfo, error) {
	return fs.RootInfo{}, errors.New("not implemented")
}
func (f *fakeRegistry) ListRoots() []fs.RootInfo          { return []fs.RootInfo{f.root} }
func (f *fakeRegistry) GetAgentPool() *agent.Pool         { return nil }
func (f *fakeRegistry) GetPreferences() *preferences.Store { return nil }
func (f *fakeRegistry) GetExternalSessionImporter(string) (agenttypes.ExternalSessionImporter, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) GetProber() *agent.Prober                  { return nil }
func (f *fakeRegistry) GetCandidateRegistry() *usecase.CandidateRegistry { return nil }
func (f *fakeRegistry) GetFileWatcher(string, *session.Manager) (*fs.SharedFileWatcher, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) ReleaseFileWatcher(string, string) {}

type fakeUsecase struct {
	sendMessage func(ctx context.Context, in usecase.SendMessageInput) error
}

func (f *fakeUsecase) CreateSession(_ context.Context, in usecase.CreateSessionInput) (*session.Session, error) {
	return &session.Session{Key: "sched-session"}, nil
}

func (f *fakeUsecase) SendMessage(ctx context.Context, in usecase.SendMessageInput) error {
	return f.sendMessage(ctx, in)
}

type noopBroadcaster struct{}

func (noopBroadcaster) BroadcastSessionMetaUpdated(string, *session.Session)    {}
func (noopBroadcaster) SetSessionPendingReply(string, string, string)           {}
func (noopBroadcaster) BroadcastSessionUserMessage(rootID, sessionKey, sessionType, sessionName, agentName, model, mode, effort, fastService string, planMode bool, content string) {
}
func (noopBroadcaster) BroadcastSessionUserMessageAt(rootID, sessionKey, sessionType, sessionName, agentName, model, mode, effort, fastService string, planMode bool, content string, timestamp time.Time, baseExchangeSeq ...int) {
}
func (noopBroadcaster) BroadcastSessionUpdate(string, string, agenttypes.Event)      {}
func (noopBroadcaster) BroadcastSessionError(string, string, string, string)         {}
func (noopBroadcaster) BroadcastSessionDone(string, string, string)                  {}
func (noopBroadcaster) BroadcastAgentStatusChanged(string)                           {}
func (noopBroadcaster) BroadcastScheduledTaskDone(string, string, string, string, string)   {}
func (noopBroadcaster) BroadcastScheduledTaskFailed(string, string, string, string, string) {}

func newRunTaskFixture(t *testing.T, send func(ctx context.Context, in usecase.SendMessageInput) error) (*Service, Task) {
	t.Helper()
	root := fs.NewRootInfo("root-1", "root-1", t.TempDir())
	manager := session.NewManager(root)
	t.Cleanup(func() { _ = manager.Shutdown() })
	svc := NewService(&fakeRegistry{root: root, manager: manager}, noopBroadcaster{})
	svc.usecase = &fakeUsecase{sendMessage: send}

	now := time.Now().UTC()
	task := Task{
		ID: "task-1", RootID: "root-1", Name: "hourly", Enabled: true,
		TaskCron: "0 * * * *", Agent: "claude", Prompt: "do it",
		TimeoutMinutes: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := NewStore(root).Save([]Task{task}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return svc, task
}

// A run that never returns must hit the timeout, record it, release the
// running lock, and let the next cron tick execute normally (R-6.1).
func TestRunTaskTimeoutFreesRunningLock(t *testing.T) {
	original := runTimeoutUnit
	runTimeoutUnit = 50 * time.Millisecond
	defer func() { runTimeoutUnit = original }()

	calls := 0
	svc, task := newRunTaskFixture(t, func(ctx context.Context, _ usecase.SendMessageInput) error {
		calls++
		if calls == 1 {
			<-ctx.Done() // hang until the run timeout fires
			return ctx.Err()
		}
		return nil
	})

	err := svc.runTask(context.Background(), task, false)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("first run error = %v, want timeout", err)
	}
	svc.mu.Lock()
	locked := svc.running["root-1:task-1"]
	svc.mu.Unlock()
	if locked {
		t.Fatal("running lock still held after timeout")
	}
	stored, err := svc.findTask("root-1", "task-1")
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if !strings.Contains(stored.LastError, "timed out") {
		t.Fatalf("LastError = %q, want timeout message", stored.LastError)
	}

	// The next tick runs normally.
	if err := svc.runTask(context.Background(), task, false); err != nil {
		t.Fatalf("second run should succeed, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("SendMessage calls = %d, want 2", calls)
	}
}

// A concurrency skip must not clobber the last real failure (R-6.2).
func TestSkipRecordsSeparatelyAndKeepsLastError(t *testing.T) {
	svc, task := newRunTaskFixture(t, func(context.Context, usecase.SendMessageInput) error { return nil })
	// Seed a real failure, then mark the task as mid-run.
	if err := svc.updateTask("root-1", "task-1", func(t *Task) { t.LastError = "real failure" }); err != nil {
		t.Fatalf("seed error: %v", err)
	}
	svc.mu.Lock()
	svc.running["root-1:task-1"] = true
	svc.mu.Unlock()

	err := svc.runTask(context.Background(), task, false)
	if err == nil || !strings.Contains(err.Error(), "previous run still active") {
		t.Fatalf("skip error = %v", err)
	}
	stored, err := svc.findTask("root-1", "task-1")
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if stored.LastError != "real failure" {
		t.Fatalf("LastError = %q, want the real failure preserved", stored.LastError)
	}
	if stored.LastSkippedAt == nil {
		t.Fatal("LastSkippedAt not recorded")
	}
}

// Task files written before the new fields must load unchanged (R-6.1 DoD).
func TestOldTaskFileStillLoadsWithDefaults(t *testing.T) {
	root := fs.NewRootInfo("root-1", "root-1", t.TempDir())
	legacy := `[{"id":"t1","root_id":"root-1","name":"old","enabled":true,"task_cron":"0 * * * *","agent":"claude","prompt":"p","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]`
	if err := root.WriteMetaFile("scheduled-agent-tasks.json", []byte(legacy)); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	tasks, err := NewStore(root).List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TimeoutMinutes != 0 || tasks[0].LastSkippedAt != nil {
		t.Fatalf("legacy task = %+v", tasks[0])
	}
	if tasks[0].runTimeout() != defaultRunTimeout {
		t.Fatalf("default timeout = %s, want %s", tasks[0].runTimeout(), defaultRunTimeout)
	}
}

// The history ring keeps exactly the newest 20 entries (R-6.3).
func TestAppendRunRecordEvictsOldest(t *testing.T) {
	task := Task{}
	for i := 0; i < 21; i++ {
		appendRunRecord(&task, RunRecord{StartedAt: time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC), OK: true})
	}
	if len(task.History) != maxRunHistory {
		t.Fatalf("history length = %d, want %d", len(task.History), maxRunHistory)
	}
	if task.History[0].StartedAt.Minute() != 1 {
		t.Fatalf("oldest entry = %v, want minute 1 (entry 0 evicted)", task.History[0].StartedAt)
	}
	if task.History[len(task.History)-1].StartedAt.Minute() != 20 {
		t.Fatalf("newest entry = %v", task.History[len(task.History)-1].StartedAt)
	}
}

// Runs must land in the task history: success and failure alike (R-6.3).
func TestRunTaskRecordsHistory(t *testing.T) {
	calls := 0
	svc, task := newRunTaskFixture(t, func(context.Context, usecase.SendMessageInput) error {
		calls++
		if calls == 1 {
			return errors.New("boom")
		}
		return nil
	})
	_ = svc.runTask(context.Background(), task, false)
	if err := svc.runTask(context.Background(), task, false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	stored, err := svc.findTask("root-1", "task-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(stored.History) != 2 {
		t.Fatalf("history = %d entries, want 2", len(stored.History))
	}
	if stored.History[0].OK || stored.History[0].Error != "boom" {
		t.Fatalf("first record = %+v, want failed boom", stored.History[0])
	}
	if !stored.History[1].OK || stored.History[1].Error != "" {
		t.Fatalf("second record = %+v, want ok", stored.History[1])
	}
}

// @daily and @every descriptors must parse, decorate, and actually fire
// (R-6.4).
func TestCronDescriptorsParseAndFire(t *testing.T) {
	send := make(chan struct{}, 8)
	svc, _ := newRunTaskFixture(t, func(context.Context, usecase.SendMessageInput) error {
		select {
		case send <- struct{}{}:
		default:
		}
		return nil
	})
	if _, err := svc.parser.Parse("@daily"); err != nil {
		t.Fatalf("@daily parse: %v", err)
	}

	// Swap the seeded task for an @every one and let the real cron fire it.
	root, _ := svc.registry.GetRoot("root-1")
	now := time.Now().UTC()
	fast := Task{
		ID: "task-fast", RootID: "root-1", Name: "fast", Enabled: true,
		TaskCron: "@every 100ms", Agent: "claude", Prompt: "p",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := NewStore(root).Save([]Task{fast}); err != nil {
		t.Fatalf("seed fast task: %v", err)
	}
	if err := svc.ReloadRoot("root-1"); err != nil {
		t.Fatalf("reload with descriptor: %v", err)
	}
	svc.cron.Start()
	defer svc.cron.Stop()
	select {
	case <-send:
	case <-time.After(3 * time.Second):
		t.Fatal("@every task never fired")
	}

	// Decorated Next/Last times share the UTC zone (R-6.4).
	decorated, err := svc.findTask("root-1", "task-fast")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	svc.decorateTask(&decorated)
	if decorated.NextRunAt == nil || decorated.NextRunAt.Location() != time.UTC {
		t.Fatalf("NextRunAt = %v, want UTC", decorated.NextRunAt)
	}
}
