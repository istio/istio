package mount

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// RunSandboxed is an opinionated function to run a command in a lightweight sandbox.
// This overrides files that are not desired in iptables execution (which runs on the node) by bind mounting them
func RunSandboxed(lockFile string, args []string) error {
	// In CNI, we are running the pod network namespace, but the host filesystem. Locking the host is both useless and harmful,
	// as it opens the risk of lock contention with other node actors (such as kube-proxy), and isn't actually needed at all.
	// Older iptables cannot turn off the lock explicitly, so we hack around it...
	// Overwrite the lock file with the network namespace file (which is assumed to be unique).
	// We are setting the lockfile to `r.NetworkNamespace`.
	// /dev/null looks like a good option, but actually doesn't work (it will ensure only one actor can access it)
	if lockFile != "" {
		if err := mount(lockFile, "/run/xtables.lock"); err != nil {
			return fmt.Errorf("bind mount of %q failed: %v", lockFile, err)
		}
	}

	// In some setups, iptables can make remote network calls(!!). Since these come from a partially initialized pod network namespace,
	// these calls can be blocked (or NetworkPolicy, etc could block them anyways).
	// This is triggered by NSS, which allows various things to use arbitrary code to lookup configuration that typically comes from files.
	// In our case, the culprit is the `xt_owner` (`-m owner`) module in iptables calls the `passwd` service to lookup the user.
	// To disallow this, bindmount /dev/null over nsswitch.conf so this never happens.
	// This should be safe to do, even if the user has an nsswitch entry that would work fine: we always use a numeric ID
	// so the passwd lookup doesn't need to succeed at all for Istio to function.
	// Effectively, we want a mini-container. In fact, running in a real container would be ideal but it is hard to do portably.
	// See https://github.com/istio/istio/issues/48416 for a real world example of this case.
	if err := mount("/dev/null", "/etc/nsswitch.conf"); err != nil {
		return fmt.Errorf("bind mount to %q failed: %v", "/etc/nsswitch.conf", err)
	}

	name := args[0]
	if filepath.Base(name) == name {
		n, err := exec.LookPath(name)
		if err != nil {
			return fmt.Errorf("failed to find command %v: %v", name, err)
		}
		name = n
	}
	return syscall.Exec(name, args, os.Environ())
}

func mount(src, dst string) error {
	return syscall.Mount(src, dst, "", syscall.MS_BIND|syscall.MS_RDONLY, "")
}
