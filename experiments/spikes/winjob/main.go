//go:build windows

// Command winjob is a throwaway spike for F3.4a (Job Object tree termination,
// assignment race, CTRL_BREAK). Not product code. Stdlib only (syscall +
// LazyDLL); the spec's golang.org/x/sys/windows is not needed for the spike.
//
//	winjob tree            one tree, close job handle, verify by PID
//	winjob race <mode> <n> mode: late | suspended | joblist
//	winjob ctrlbreak       CTRL_BREAK to a new process group
//	winjob nested          assign into a job while we are already in a job
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	k32                       = syscall.NewLazyDLL("kernel32.dll")
	pCreateJobObject          = k32.NewProc("CreateJobObjectW")
	pSetInformationJobObject  = k32.NewProc("SetInformationJobObject")
	pQueryInformationJobObj   = k32.NewProc("QueryInformationJobObject")
	pAssignProcessToJobObject = k32.NewProc("AssignProcessToJobObject")
	pIsProcessInJob           = k32.NewProc("IsProcessInJob")
	pTerminateJobObject       = k32.NewProc("TerminateJobObject")
	pOpenThread               = k32.NewProc("OpenThread")
	pResumeThread             = k32.NewProc("ResumeThread")
	pSnapshot                 = k32.NewProc("CreateToolhelp32Snapshot")
	pThread32First            = k32.NewProc("Thread32First")
	pThread32Next             = k32.NewProc("Thread32Next")
	pGenerateCtrlEvent        = k32.NewProc("GenerateConsoleCtrlEvent")
	pGetConsoleWindow         = k32.NewProc("GetConsoleWindow")
	pInitAttrList             = k32.NewProc("InitializeProcThreadAttributeList")
	pUpdateAttr               = k32.NewProc("UpdateProcThreadAttribute")
	pCreateProcessW           = k32.NewProc("CreateProcessW")
)

const (
	killOnClose     = 0x2000
	extendedLimit   = 9
	basicAccounting = 1
	createSuspended = 0x4
	newProcGroup    = 0x200
	ctrlBreakEvent  = 1
	attrJobList     = 0x0002000D
	extStartupInfo  = 0x80000
)

type jobExtLimit struct {
	PerProcessUserTimeLimit, PerJobUserTimeLimit int64
	LimitFlags                                   uint32
	MinWS, MaxWS                                 uintptr
	ActiveProcessLimit                           uint32
	Affinity                                     uintptr
	PriorityClass, SchedulingClass               uint32
	IoInfo                                       [6]uint64
	ProcessMemoryLimit, JobMemoryLimit           uintptr
	PeakProcessMemory, PeakJobMemory             uintptr
}

type jobAccounting struct {
	TotalUser, TotalKernel, PeriodUser, PeriodKernel int64
	PageFaults, TotalProcesses, ActiveProcesses      uint32
	TotalTerminated                                  uint32
}

func newKillOnCloseJob() (syscall.Handle, error) {
	h, _, e := pCreateJobObject.Call(0, 0)
	if h == 0 {
		return 0, fmt.Errorf("CreateJobObject: %w", e)
	}
	var l jobExtLimit
	l.LimitFlags = killOnClose
	r, _, e := pSetInformationJobObject.Call(h, extendedLimit, uintptr(unsafe.Pointer(&l)), unsafe.Sizeof(l))
	if r == 0 {
		return 0, fmt.Errorf("SetInformationJobObject: %w", e)
	}
	return syscall.Handle(h), nil
}

func activeInJob(job syscall.Handle) (uint32, error) {
	var a jobAccounting
	r, _, e := pQueryInformationJobObj.Call(uintptr(job), basicAccounting, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
	if r == 0 {
		return 0, e
	}
	return a.ActiveProcesses, nil
}

func openProc(pid uint32) (syscall.Handle, error) {
	// SET_QUOTA|TERMINATE|SYNCHRONIZE|QUERY_LIMITED_INFORMATION
	return syscall.OpenProcess(0x0100|0x0001|0x00100000|0x1000, false, pid)
}

func alive(h syscall.Handle) bool {
	ev, _ := syscall.WaitForSingleObject(h, 0)
	return ev == uint32(syscall.WAIT_TIMEOUT)
}

func assign(job, proc syscall.Handle) error {
	r, _, e := pAssignProcessToJobObject.Call(uintptr(job), uintptr(proc))
	if r == 0 {
		return fmt.Errorf("AssignProcessToJobObject: %w", e)
	}
	return nil
}

func resumeMainThread(pid uint32) error {
	snap, _, e := pSnapshot.Call(4, 0)
	if snap == uintptr(syscall.InvalidHandle) {
		return e
	}
	defer syscall.CloseHandle(syscall.Handle(snap))
	var te [7]uint32 // size, usage, tid, owner, basepri, deltapri, flags
	te[0] = uint32(unsafe.Sizeof(te))
	r, _, _ := pThread32First.Call(snap, uintptr(unsafe.Pointer(&te)))
	for r != 0 {
		if te[3] == pid {
			th, _, e := pOpenThread.Call(0x2, 0, uintptr(te[2]))
			if th == 0 {
				return e
			}
			defer syscall.CloseHandle(syscall.Handle(th))
			n, _, e := pResumeThread.Call(th)
			if n == 0xFFFFFFFF {
				return e
			}
			return nil
		}
		r, _, _ = pThread32Next.Call(snap, uintptr(unsafe.Pointer(&te)))
	}
	return fmt.Errorf("no thread for pid %d", pid)
}

// startChild runs `self child <dir>` per the chosen assignment strategy and
// returns the child's process handle.
func startChild(mode string, job syscall.Handle, dir string, extra ...string) (uint32, syscall.Handle, error) {
	self, _ := os.Executable()
	args := append([]string{"child", dir}, extra...)
	switch mode {
	case "joblist":
		return createWithJobList(self, args, job)
	case "suspended", "late":
		cmd := exec.Command(self, args...)
		flags := uint32(newProcGroup)
		if mode == "suspended" {
			flags |= createSuspended
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		if err := cmd.Start(); err != nil {
			return 0, 0, err
		}
		pid := uint32(cmd.Process.Pid)
		h, err := openProc(pid)
		if err != nil {
			return 0, 0, err
		}
		if d := os.Getenv("SPIKE_ASSIGN_DELAY_MS"); mode == "late" && d != "" {
			ms, _ := strconv.Atoi(d)
			time.Sleep(time.Duration(ms) * time.Millisecond) // widen the race window deterministically
		}
		if err := assign(job, h); err != nil {
			return 0, 0, err
		}
		if mode == "suspended" {
			if err := resumeMainThread(pid); err != nil {
				return 0, 0, err
			}
		}
		go cmd.Wait() // reap so the Go handle is released
		return pid, h, nil
	}
	return 0, 0, fmt.Errorf("mode %q", mode)
}

type startupInfoEx struct {
	syscall.StartupInfo
	AttrList uintptr
}

// createWithJobList is strategy (b): the process is born inside the job via
// PROC_THREAD_ATTRIBUTE_JOB_LIST, so there is no window in which it is outside.
func createWithJobList(exe string, args []string, job syscall.Handle) (uint32, syscall.Handle, error) {
	var size uintptr
	pInitAttrList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	buf := make([]byte, size)
	if r, _, e := pInitAttrList.Call(uintptr(unsafe.Pointer(&buf[0])), 1, 0, uintptr(unsafe.Pointer(&size))); r == 0 {
		return 0, 0, fmt.Errorf("InitializeProcThreadAttributeList: %w", e)
	}
	jobs := [1]syscall.Handle{job}
	if r, _, e := pUpdateAttr.Call(uintptr(unsafe.Pointer(&buf[0])), 0, attrJobList,
		uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs), 0, 0); r == 0 {
		return 0, 0, fmt.Errorf("UpdateProcThreadAttribute(JOB_LIST): %w", e)
	}
	si := startupInfoEx{AttrList: uintptr(unsafe.Pointer(&buf[0]))}
	si.Cb = uint32(unsafe.Sizeof(si))
	cl, _ := syscall.UTF16PtrFromString(syscall.EscapeArg(exe) + " " + strings.Join(escapeAll(args), " "))
	var pi syscall.ProcessInformation
	r, _, e := pCreateProcessW.Call(0, uintptr(unsafe.Pointer(cl)), 0, 0, 0,
		extStartupInfo|newProcGroup, 0, 0, uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		return 0, 0, fmt.Errorf("CreateProcessW: %w", e)
	}
	syscall.CloseHandle(pi.Thread)
	pid := pi.ProcessId
	syscall.CloseHandle(pi.Process)
	h, err := openProc(pid)
	return pid, h, err
}

func escapeAll(a []string) []string {
	o := make([]string, len(a))
	for i, s := range a {
		o[i] = syscall.EscapeArg(s)
	}
	return o
}

func waitFile(path string, d time.Duration) (string, bool) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b)), true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return "", false
}

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	switch os.Args[1] {
	case "child":
		child()
	case "sleep":
		time.Sleep(60 * time.Second)
	case "tree":
		tree()
	case "race":
		n, _ := strconv.Atoi(os.Args[3])
		race(os.Args[2], n)
	case "ctrlbreak":
		ctrlBreak()
	case "nested":
		nested()
	case "detached":
		detached()
	}
}

// child spawns a grandchild first thing (the worst case for the assignment
// race), records both pids, then sleeps (or handles CTRL_BREAK in "trap" mode).
func child() {
	dir := os.Args[2]
	self, _ := os.Executable()
	gc := exec.Command(self, "sleep")
	if err := gc.Start(); err != nil {
		os.Exit(3)
	}
	os.WriteFile(filepath.Join(dir, "gc.pid"), []byte(strconv.Itoa(gc.Process.Pid)), 0o644)
	if len(os.Args) > 3 && os.Args[3] == "trap" {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		os.WriteFile(filepath.Join(dir, "ready"), []byte("1"), 0o644)
		<-c
		os.WriteFile(filepath.Join(dir, "got-break"), []byte("1"), 0o644)
		os.Exit(0)
	}
	time.Sleep(60 * time.Second)
}

func inJob(h syscall.Handle, job syscall.Handle) bool {
	var res int32
	pIsProcessInJob.Call(uintptr(h), uintptr(job), uintptr(unsafe.Pointer(&res)))
	return res != 0
}

func tree() {
	dir, _ := os.MkdirTemp("", "wj-")
	defer os.RemoveAll(dir)
	job, err := newKillOnCloseJob()
	if err != nil {
		fmt.Println(err)
		return
	}
	pid, h, err := startChild("suspended", job, dir)
	if err != nil {
		fmt.Println("start:", err)
		return
	}
	gcs, ok := waitFile(filepath.Join(dir, "gc.pid"), 5*time.Second)
	if !ok {
		fmt.Println("grandchild never reported")
		return
	}
	gpid, _ := strconv.Atoi(gcs)
	gh, err := openProc(uint32(gpid))
	if err != nil {
		fmt.Println("open gc:", err)
		return
	}
	n, _ := activeInJob(job)
	fmt.Printf("child pid=%d grandchild pid=%d; grandchild in job=%v; ActiveProcesses=%d\n", pid, gpid, inJob(gh, job), n)
	fmt.Printf("before close: child alive=%v grandchild alive=%v\n", alive(h), alive(gh))
	t0 := time.Now()
	syscall.CloseHandle(job) // KILL_ON_JOB_CLOSE
	for alive(h) || alive(gh) {
		if time.Since(t0) > 5*time.Second {
			break
		}
		time.Sleep(time.Millisecond)
	}
	fmt.Printf("after close (%v): child alive=%v grandchild alive=%v\n", time.Since(t0).Round(time.Millisecond), alive(h), alive(gh))
	var code uint32
	syscall.GetExitCodeProcess(h, &code)
	fmt.Printf("child exit code after job close: %d (0x%X)\n", code, code)
	if alive(gh) {
		syscall.TerminateProcess(gh, 1)
	}
	if alive(h) {
		syscall.TerminateProcess(h, 1)
	}
}

func race(mode string, n int) {
	escaped, failed := 0, 0
	for i := 0; i < n; i++ {
		dir, _ := os.MkdirTemp("", "wr-")
		job, err := newKillOnCloseJob()
		if err != nil {
			fmt.Println(err)
			return
		}
		_, h, err := startChild(mode, job, dir)
		if err != nil {
			failed++
			syscall.CloseHandle(job)
			os.RemoveAll(dir)
			if failed == 1 {
				fmt.Println("start error:", err)
			}
			continue
		}
		gcs, ok := waitFile(filepath.Join(dir, "gc.pid"), 5*time.Second)
		if !ok {
			failed++
		} else {
			gpid, _ := strconv.Atoi(gcs)
			gh, _ := openProc(uint32(gpid))
			syscall.CloseHandle(job)
			deadline := time.Now().Add(2 * time.Second)
			for alive(gh) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if alive(gh) {
				escaped++
				syscall.TerminateProcess(gh, 1)
			}
			syscall.CloseHandle(gh)
		}
		if alive(h) {
			syscall.TerminateProcess(h, 1)
		}
		syscall.CloseHandle(h)
		os.RemoveAll(dir)
	}
	fmt.Printf("mode=%s iterations=%d grandchild escaped=%d start/other failures=%d\n", mode, n, escaped, failed)
}

func ctrlBreak() {
	cw, _, _ := pGetConsoleWindow.Call()
	fmt.Printf("has console window: %v\n", cw != 0)
	dir, _ := os.MkdirTemp("", "wc-")
	defer os.RemoveAll(dir)
	self, _ := os.Executable()
	cmd := exec.Command(self, "child", dir, "trap")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: newProcGroup}
	if err := cmd.Start(); err != nil {
		fmt.Println(err)
		return
	}
	if _, ok := waitFile(filepath.Join(dir, "ready"), 5*time.Second); !ok {
		fmt.Println("child not ready")
		return
	}
	// Ignore CTRL_BREAK in ourselves in case it leaks to our own group.
	signal.Ignore(os.Interrupt)
	r, _, e := pGenerateCtrlEvent.Call(ctrlBreakEvent, uintptr(cmd.Process.Pid))
	fmt.Printf("GenerateConsoleCtrlEvent(CTRL_BREAK, pid=%d) ok=%v err=%v\n", cmd.Process.Pid, r != 0, e)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		_, hit := os.Stat(filepath.Join(dir, "got-break"))
		fmt.Printf("child exited err=%v handler ran=%v\n", err, hit == nil)
	case <-time.After(3 * time.Second):
		fmt.Println("child did NOT exit within 3s of CTRL_BREAK; force-killing")
		cmd.Process.Kill()
		<-done
	}
	if gcs, ok := waitFile(filepath.Join(dir, "gc.pid"), time.Second); ok {
		gpid, _ := strconv.Atoi(gcs)
		if gh, err := openProc(uint32(gpid)); err == nil {
			fmt.Printf("grandchild alive after break (no job): %v\n", alive(gh))
			syscall.TerminateProcess(gh, 1)
		}
	}
}

func nested() {
	cur, _ := syscall.GetCurrentProcess()
	fmt.Printf("this process already in some job: %v\n", inJob(cur, 0))
	tree()
}

// detached runs ctrlbreak in a process with no console (DETACHED_PROCESS),
// like a desktop sidecar or service.
func detached() {
	self, _ := os.Executable()
	out := filepath.Join(os.TempDir(), "wj-detached.txt")
	f, _ := os.Create(out)
	cmd := exec.Command(self, "ctrlbreak")
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x8}
	if err := cmd.Run(); err != nil {
		fmt.Println("run:", err)
	}
	f.Close()
	b, _ := os.ReadFile(out)
	fmt.Print(string(b))
	os.Remove(out)
}
