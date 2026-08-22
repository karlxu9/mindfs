//go:build windows

package acp

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func killProcessTree(proc *os.Process) error {
	if proc == nil {
		return nil
	}
	// ACP agents spawn grandchildren (node -> agent -> tools); proc.Kill only
	// takes the direct child, so use taskkill /T for the whole tree, mirroring
	// commandexec's Windows KillTree.
	kill := exec.Command("taskkill", "/PID", strconv.Itoa(proc.Pid), "/T", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := kill.Run(); err != nil {
		return proc.Kill()
	}
	return nil
}

func configurePlatformProcessCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windowsACPProcessCreationFlags(),
	}
}
