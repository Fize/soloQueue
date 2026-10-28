package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type runtimeJob struct {
	sync.Mutex
	handle windows.Handle
}

var runtimeJobs sync.Map // map[*exec.Cmd]*runtimeJob

func Configure(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
}

// Start assigns the still-suspended process to a job before
// resuming its first thread, so it cannot spawn children outside the job.
func Start(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create process job: %w", err)
	}
	state := &runtimeJob{handle: job}
	state.Lock()
	runtimeJobs.Store(cmd, state)
	if err = cmd.Start(); err != nil {
		runtimeJobs.Delete(cmd)
		state.Unlock()
		windows.CloseHandle(job)
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		windows.CloseHandle(process)
	}
	if err == nil {
		err = resumeRuntimeThread(uint32(cmd.Process.Pid))
	}
	if err != nil {
		_ = cmd.Process.Kill()
		state.Unlock()
		_ = cmd.Wait()
		Close(cmd)
		return fmt.Errorf("attach process to job: %w", err)
	}
	state.Unlock()
	return nil
}

func resumeRuntimeThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if openErr != nil {
			return openErr
		}
		_, resumeErr := windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		return resumeErr
	}
	return fmt.Errorf("suspended process thread not found: %w", err)
}

func Kill(cmd *exec.Cmd) error {
	if value, ok := runtimeJobs.Load(cmd); ok {
		state := value.(*runtimeJob)
		state.Lock()
		defer state.Unlock()
		if state.handle != 0 {
			return windows.TerminateJobObject(state.handle, 1)
		}
	}
	if cmd.Process == nil {
		return nil
	}
	err := cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func Close(cmd *exec.Cmd) {
	value, ok := runtimeJobs.LoadAndDelete(cmd)
	if !ok {
		return
	}
	state := value.(*runtimeJob)
	state.Lock()
	defer state.Unlock()
	if state.handle != 0 {
		_ = windows.CloseHandle(state.handle)
		state.handle = 0
	}
}
