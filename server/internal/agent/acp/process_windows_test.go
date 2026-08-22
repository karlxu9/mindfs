package acp

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func queryChildPIDs(t *testing.T, parentPID int) []string {
	t.Helper()
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_Process -Filter 'ParentProcessId="+strconv.Itoa(parentPID)+"').ProcessId").Output()
	if err != nil {
		t.Fatalf("query children of %d: %v", parentPID, err)
	}
	return strings.Fields(string(out))
}

func isProcessAlive(t *testing.T, pid string) bool {
	t.Helper()
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"if (Get-Process -Id "+pid+" -ErrorAction SilentlyContinue) { 'alive' } else { 'dead' }").Output()
	if err != nil {
		t.Fatalf("probe pid %s: %v", pid, err)
	}
	return strings.Contains(string(out), "alive")
}

// killProcessTree claimed to kill the tree but only killed the direct child,
// leaving ACP grandchildren orphaned (R-1.1 acceptance 2). Verify the whole
// tree dies: cmd.exe parent plus the ping child it spawns.
func TestKillProcessTreeKillsGrandchildren(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/C", "ping -n 60 127.0.0.1 >NUL")
	configurePlatformProcessCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	var children []string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		children = queryChildPIDs(t, cmd.Process.Pid)
		if len(children) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(children) == 0 {
		t.Skip("cmd child process never appeared; environment cannot host the fixture")
	}

	if err := killProcessTree(cmd.Process); err != nil {
		t.Fatalf("killProcessTree: %v", err)
	}
	_, _ = cmd.Process.Wait()

	for _, pid := range children {
		alive := true
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			if alive = isProcessAlive(t, pid); !alive {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if alive {
			t.Fatalf("grandchild pid %s survived killProcessTree", pid)
		}
	}
}
