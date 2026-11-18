//go:build linux

// Command linuxsandbox is a throwaway spike for F3.9 (Landlock write
// confinement, network namespaces). It is not product code: nothing imports it.
//
//	go run ./experiments/spikes/linuxsandbox probe
//	go run ./experiments/spikes/linuxsandbox landlock
//	go run ./experiments/spikes/linuxsandbox netns
package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446

	landlockCreateRulesetVersion = 1 << 0
	landlockRulePathBeneath      = 1
	prSetNoNewPrivs              = 38
	oPath                        = 0x200000 // O_PATH is not exported by package syscall
)

// Write-class access rights per Landlock ABI. Read and execute are
// deliberately not handled: F3.9 v0 has no read confinement.
func writeRights(abi int) uint64 {
	r := uint64(1<<1 | 1<<4 | 1<<5 | 1<<6 | 1<<7 | 1<<8 | 1<<9 | 1<<10 | 1<<11 | 1<<12)
	if abi >= 2 {
		r |= 1 << 13 // REFER
	}
	if abi >= 3 {
		r |= 1 << 14 // TRUNCATE
	}
	return r
}

func landlockABI() (int, error) {
	v, _, e := syscall.Syscall(sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	if e != 0 {
		return 0, e
	}
	return int(v), nil
}

type rulesetAttr struct{ handledAccessFS uint64 }
type pathBeneath struct {
	allowedAccess uint64
	parentFd      int32
	_             int32
}

func restrictWrites(dirs []string) error {
	abi, err := landlockABI()
	if err != nil {
		return fmt.Errorf("landlock_create_ruleset(version): %w", err)
	}
	rights := writeRights(abi)
	attr := rulesetAttr{rights}
	fd, _, e := syscall.Syscall(sysLandlockCreateRuleset, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if e != 0 {
		return fmt.Errorf("landlock_create_ruleset: %w", e)
	}
	defer syscall.Close(int(fd))
	for _, d := range dirs {
		dfd, err := syscall.Open(d, oPath|syscall.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("open %s: %w", d, err)
		}
		granted := rights
		var st syscall.Stat_t
		if err := syscall.Fstat(dfd, &st); err == nil && st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
			// A rule on a non-directory may carry file rights only, else EINVAL.
			granted &= 1<<1 | 1<<14
		}
		pb := pathBeneath{allowedAccess: granted, parentFd: int32(dfd)}
		_, _, e := syscall.Syscall(sysLandlockAddRule, fd, landlockRulePathBeneath, uintptr(unsafe.Pointer(&pb)))
		syscall.Close(dfd)
		if e != 0 {
			return fmt.Errorf("landlock_add_rule %s: %w", d, e)
		}
	}
	if _, _, e := syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); e != 0 {
		return fmt.Errorf("PR_SET_NO_NEW_PRIVS: %w", e)
	}
	if _, _, e := syscall.Syscall(sysLandlockRestrictSelf, fd, 0, 0); e != 0 {
		return fmt.Errorf("landlock_restrict_self: %w", e)
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe|landlock|netns")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "probe":
		probe()
	case "landlock":
		landlockDemo()
	case "helper":
		helper() // internal: helper <allowed-dir>,<allowed-dir> -- cmd args...
	case "netns":
		netnsDemo()
	case "netchild":
		netChild()
	default:
		os.Exit(2)
	}
}

func probe() {
	abi, err := landlockABI()
	fmt.Printf("landlock ABI: %d err=%v\n", abi, err)
	b, e := os.ReadFile("/sys/kernel/security/lsm")
	fmt.Printf("lsm list: %s err=%v\n", b, e)
	for _, p := range []string{
		"/proc/sys/kernel/apparmor_restrict_unprivileged_userns",
		"/proc/sys/kernel/unprivileged_userns_clone",
		"/proc/sys/user/max_user_namespaces",
		"/proc/sys/kernel/unprivileged_userns_apparmor_policy",
	} {
		b, e := os.ReadFile(p)
		fmt.Printf("%s = %q err=%v\n", p, b, e)
	}
}

// helper is the re-exec stage: restrict this thread, then exec the command so
// the Landlock domain and NO_NEW_PRIVS are inherited by the shell.
func helper() {
	runtime.LockOSThread()
	var dirs []string
	i := 2
	for ; i < len(os.Args) && os.Args[i] != "--"; i++ {
		dirs = append(dirs, os.Args[i])
	}
	argv := os.Args[i+1:]
	if err := restrictWrites(dirs); err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		os.Exit(125)
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper:", err)
		os.Exit(127)
	}
	err = syscall.Exec(path, argv, os.Environ())
	fmt.Fprintln(os.Stderr, "helper exec:", err)
	os.Exit(126)
}

func landlockDemo() {
	allowed, _ := os.MkdirTemp("", "ll-allowed-")
	outside, _ := os.MkdirTemp("", "ll-outside-")
	defer os.RemoveAll(allowed)
	defer os.RemoveAll(outside)
	self, _ := os.Executable()
	script := `
echo in  > "$A/in.txt";      echo "write inside:   rc=$?"
echo out > "$O/out.txt";     echo "write outside:  rc=$?"
mkdir "$O/d";                echo "mkdir outside:  rc=$?"
mkdir "$A/d";                echo "mkdir inside:   rc=$?"
echo x > /dev/null;          echo "write /dev/null: rc=$?"
cat /etc/hostname >/dev/null; echo "read /etc:     rc=$?"
(echo y >> "$O/out.txt") ;   echo "append outside: rc=$?"
rm "$A/in.txt";              echo "rm inside:      rc=$?"
`
	cmd := exec.Command(self, "helper", allowed, "/dev/null", "--", "sh", "-c", script)
	cmd.Env = append(os.Environ(), "A="+allowed, "O="+outside)
	out, err := cmd.CombinedOutput()
	fmt.Printf("%s(exit err=%v)\n", out, err)
	_, statErr := os.Stat(filepath.Join(outside, "out.txt"))
	fmt.Printf("outside file exists after run: %v\n", statErr == nil)
}

func netnsDemo() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen:", err)
		return
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	addr := ln.Addr().String()
	self, _ := os.Executable()

	ctl := exec.Command(self, "netchild", addr)
	out, err := ctl.CombinedOutput()
	fmt.Printf("control (no namespace): %s err=%v\n", out, err)

	uid, gid := os.Getuid(), os.Getgid()
	ns := exec.Command(self, "netchild", addr)
	ns.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: uid, Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: gid, Size: 1}},
	}
	out, err = ns.CombinedOutput()
	fmt.Printf("userns+netns: %s err=%v\n", out, err)

	// Netns only, no user namespace (needs CAP_SYS_ADMIN; expected EPERM).
	n2 := exec.Command(self, "netchild", addr)
	n2.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNET}
	out, err = n2.CombinedOutput()
	fmt.Printf("netns only: %s err=%v\n", out, err)
}

func netChild() {
	ifs, _ := net.Interfaces()
	var names []string
	for _, i := range ifs {
		names = append(names, i.Name+"(flags="+i.Flags.String()+")")
	}
	fmt.Printf("uid=%d interfaces=%v ", os.Getuid(), names)
	c, err := net.Dial("tcp", os.Args[2])
	if err != nil {
		fmt.Printf("connect FAILED: %v", err)
		os.Exit(1)
	}
	c.Close()
	fmt.Print("connect OK")
	_ = strconv.Itoa
}
