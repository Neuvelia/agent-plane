//go:build windows

package process

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Supervise creates the target suspended, assigns it to a kill-on-close Job,
// and only then resumes its primary thread. No target instruction runs before
// ownership is established. The control pipe is absent from inherited handles.
func Supervise(control io.Reader, stdout, stderr io.Writer) int {
	statusFile := statusFileFromEnvironment()
	status := targetStatus{Code: -1, Failure: "supervisor"}
	defer func() {
		writeTargetStatus(statusFile, status)
		if statusFile != nil {
			_ = statusFile.Close()
		}
	}()
	decoder := json.NewDecoder(io.LimitReader(control, maxRequestBytes+1))
	decoder.DisallowUnknownFields()
	var request wireSpec
	if err := decoder.Decode(&request); err != nil || request.Spec.Path == "" || request.ExtraFileCount != 0 {
		return 125
	}
	lost := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, control); close(lost) }()
	select {
	case <-lost:
		return 125
	default:
	}
	return superviseWindowsStatus(request.Spec, lost, stdout, stderr, nil, &status)
}

func superviseWindows(spec Spec, lost <-chan struct{}, stdout, stderr io.Writer, beforeAssign func(windows.Handle)) int {
	return superviseWindowsStatus(spec, lost, stdout, stderr, beforeAssign, nil)
}

func superviseWindowsStatus(spec Spec, lost <-chan struct{}, stdout, stderr io.Writer, beforeAssign func(windows.Handle), status *targetStatus) int {
	path, err := windowsTargetPath(spec.Path, spec.Dir)
	if err != nil {
		windowsStartFailure(status, spec.Dir)
		return 125
	}
	application, err := windows.UTF16PtrFromString(path)
	if err != nil {
		windowsStartFailure(status, spec.Dir)
		return 125
	}
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{path}, spec.Args...)))
	if err != nil {
		windowsStartFailure(status, spec.Dir)
		return 125
	}
	var directory *uint16
	if spec.Dir != "" {
		directory, err = windows.UTF16PtrFromString(spec.Dir)
		if err != nil {
			windowsStartFailure(status, spec.Dir)
			return 125
		}
	}
	// An explicit empty block prevents ambient supervisor values from entering
	// a target with no configured environment references.
	env := windowsTargetEnvironment(spec.Env)
	block := make([]uint16, 0)
	for _, value := range env {
		encoded, err := windows.UTF16FromString(value)
		if err != nil {
			return 125
		}
		block = append(block, encoded...)
	}
	if len(env) == 0 {
		block = append(block, 0)
	}
	block = append(block, 0)
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return 125
	}
	defer stdinRead.Close()
	defer stdinWrite.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return 125
	}
	defer stdoutRead.Close()
	defer stdoutWrite.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		return 125
	}
	defer stderrRead.Close()
	defer stderrWrite.Close()

	var inherited [3]windows.Handle
	files := []*os.File{stdinRead, stdoutWrite, stderrWrite}
	for i, file := range files {
		if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(file.Fd()), windows.CurrentProcess(), &inherited[i], 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			for _, h := range inherited {
				if h != 0 {
					_ = windows.CloseHandle(h)
				}
			}
			return 125
		}
	}
	defer func() {
		for i, h := range inherited {
			if h != 0 {
				_ = windows.CloseHandle(h)
				inherited[i] = 0
			}
		}
	}()
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 125
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&inherited[0]), unsafe.Sizeof(inherited)); err != nil {
		return 125
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:       uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:    windows.STARTF_USESTDHANDLES,
			StdInput: inherited[0], StdOutput: inherited[1], StdErr: inherited[2],
		},
		ProcThreadAttributeList: attributes.List(),
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 125
	}
	defer windows.CloseHandle(job)
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return 125
	}
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(application, command, nil, nil, true, flags, &block[0], directory, &startup.StartupInfo, &process); err != nil {
		windowsStartFailure(status, spec.Dir)
		return 125
	}
	// CreateProcess duplicated the three handles into the suspended child.
	// Retaining our inheritable copies keeps output pipes and stdin alive even
	// after the Job terminates, so drop them before any copy worker starts.
	for i, h := range inherited {
		_ = windows.CloseHandle(h)
		inherited[i] = 0
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)
	if beforeAssign != nil {
		beforeAssign(process.Process)
	}
	if err := windows.AssignProcessToJobObject(job, process.Process); err != nil {
		_ = windows.TerminateProcess(process.Process, 125)
		return 125
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		return 125
	}
	if status != nil {
		status.Started = true
	}
	// Drop the supervisor copies of child pipe ends so output EOF reflects
	// only handles in the owned Job.
	_ = stdinRead.Close()
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	stdinDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(stdinWrite, bytes.NewReader(spec.Stdin))
		_ = stdinWrite.Close()
		stdinDone <- err
	}()
	outputDone := make(chan error, 2)
	go func() { _, err := io.Copy(stdout, stdoutRead); outputDone <- err }()
	go func() { _, err := io.Copy(stderr, stderrRead); outputDone <- err }()
	wait := make(chan error, 1)
	go func() { _, err := windows.WaitForSingleObject(process.Process, windows.INFINITE); wait <- err }()
	select {
	case <-lost:
		if !terminateAndJoinJob(job) {
			return 125
		}
		<-wait
		if status != nil {
			status.Failure = "cancelled"
		}
		return 125
	case err := <-wait:
		if err != nil {
			_ = terminateAndJoinJob(job)
			return 125
		}
		// The leader may leave descendants holding output handles. Terminate
		// the Job before joining copy workers, closing those handles.
		var code uint32
		if err := windows.GetExitCodeProcess(process.Process, &code); err != nil {
			return 125
		}
		if !terminateAndJoinJob(job) {
			return 125
		}
		stdinErr, stdoutErr, stderrErr := <-stdinDone, <-outputDone, <-outputDone
		if stdinErr != nil || stdoutErr != nil || stderrErr != nil {
			return 125
		}
		if status != nil {
			status.Code, status.Failure = int(code), ""
		}
		return int(code)
	}
}

func windowsStartFailure(status *targetStatus, dir string) {
	if status != nil {
		status.Failure = "start"
		status.Reason = "executable is unavailable"
		if dir != "" {
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				status.Reason = "working directory is unavailable"
			}
		}
	}
}

func windowsTargetPath(path, dir string) (string, error) {
	if dir != "" && !filepath.IsAbs(path) && strings.ContainsAny(path, `\\/`) {
		path = filepath.Join(dir, path)
	}
	return exec.LookPath(path)
}

func windowsTargetEnvironment(values []string) []string {
	last := make(map[string]int, len(values))
	for i, value := range values {
		last[strings.ToUpper(windowsEnvName(value))] = i
	}
	env := make([]string, 0, len(values)+1)
	for i, value := range values {
		if last[strings.ToUpper(windowsEnvName(value))] == i {
			env = append(env, value)
		}
	}
	if _, ok := last["SYSTEMROOT"]; !ok {
		// Go's direct exec path always forwards this Windows loader variable.
		env = append(env, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
	}
	sort.Slice(env, func(i, j int) bool {
		return strings.ToUpper(windowsEnvName(env[i])) < strings.ToUpper(windowsEnvName(env[j]))
	})
	return env
}

type jobAccounting struct {
	TotalUserTime, TotalKernelTime                                                 int64
	ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime                             int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses uint32
}

// TerminateJobObject can return before descendant processes are fully gone.
// Join the Job's active-process count before reporting cancellation complete.
func terminateAndJoinJob(job windows.Handle) bool {
	if err := windows.TerminateJobObject(job, 125); err != nil {
		return false
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var count jobAccounting
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&count)), uint32(unsafe.Sizeof(count)), nil); err != nil {
			return false
		}
		if count.ActiveProcesses == 0 {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func windowsEnvName(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] == '=' {
			return value[:i]
		}
	}
	return value
}
