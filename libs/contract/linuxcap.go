package contract

import (
	"fmt"
	"slices"
	"strings"
)

// kernelLinuxCaps are the kernel's Linux capability names, bits 0 to 40, upper case without the CAP_
// prefix (spec 0022). Add a name here by hand when a kernel adds one.
var kernelLinuxCaps = []string{
	"CHOWN", "DAC_OVERRIDE", "DAC_READ_SEARCH", "FOWNER", "FSETID", "KILL", "SETGID", "SETUID",
	"SETPCAP", "LINUX_IMMUTABLE", "NET_BIND_SERVICE", "NET_BROADCAST", "NET_ADMIN", "NET_RAW",
	"IPC_LOCK", "IPC_OWNER", "SYS_MODULE", "SYS_RAWIO", "SYS_CHROOT", "SYS_PTRACE", "SYS_PACCT",
	"SYS_ADMIN", "SYS_BOOT", "SYS_NICE", "SYS_RESOURCE", "SYS_TIME", "SYS_TTY_CONFIG", "MKNOD",
	"LEASE", "AUDIT_WRITE", "AUDIT_CONTROL", "SETFCAP", "MAC_OVERRIDE", "MAC_ADMIN", "SYSLOG",
	"WAKE_ALARM", "BLOCK_SUSPEND", "AUDIT_READ", "PERFMON", "BPF", "CHECKPOINT_RESTORE",
}

// IsLinuxCap reports whether name is one of the kernel's Linux capability names, written upper
// case without the CAP_ prefix (spec 0022).
func IsLinuxCap(name string) bool {
	return slices.Contains(kernelLinuxCaps, name)
}

// DeployableLinuxCap reports whether a module in a deployed graph may add the Linux capability
// name (spec 0022): only NET_BIND_SERVICE, compared exactly.
func DeployableLinuxCap(name string) bool {
	return name == "NET_BIND_SERVICE"
}

// UndeployableLinuxCaps lists, in entry order, every hardening.cap_add name a deployed graph
// refuses, invalid names included; nil when there are none (spec 0022).
func (m Module) UndeployableLinuxCaps() []string {
	if m.Hardening == nil {
		return nil
	}
	var out []string
	for _, name := range m.Hardening.CapAdd {
		if !DeployableLinuxCap(name) {
			out = append(out, name)
		}
	}
	return out
}

// linuxCaps checks hardening.cap_add (spec 0022): each item a kernel name in its one spelling,
// never ALL, and listed once.
func (c *moduleChecker) linuxCaps() {
	if c.m.Hardening == nil {
		return
	}
	first := map[string]int{}
	for n, name := range c.m.Hardening.CapAdd {
		p := fmt.Sprintf("hardening.cap_add.%d", n)
		switch {
		case name == "ALL":
			c.add(p, CodeInvalidHardening, "ALL is not allowed here; name each Linux capability")
		case !IsLinuxCap(name):
			msg := fmt.Sprintf("%q is not a Linux capability name", name)
			if known := strings.TrimPrefix(strings.ToUpper(name), "CAP_"); IsLinuxCap(known) {
				msg += "; write " + known
			}
			c.add(p, CodeInvalidHardening, "%s", msg)
		default:
			if at, seen := first[name]; seen {
				c.add(p, CodeInvalidHardening, "%s is already listed at hardening.cap_add.%d", name, at)
				continue
			}
			first[name] = n
		}
	}
}
