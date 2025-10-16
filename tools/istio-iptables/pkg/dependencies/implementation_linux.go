// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dependencies

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	netns "github.com/containernetworking/plugins/pkg/ns"
	"golang.org/x/sys/unix"
	utilversion "k8s.io/apimachinery/pkg/util/version"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

var testRuleAdd = []string{"-t", "filter", "-A", "INPUT", "-p", "255", "-j", "DROP"}

// TODO the entire `istio-iptables` package is linux-specific, I'm not sure we really need
// platform-differentiators for the `dependencies` package itself.

// NoLocks returns true if this version does not use or support locks
func (v IptablesVersion) NoLocks() bool {
	// nf_tables does not use locks
	// legacy added locks in 1.6.2
	return !v.Legacy || v.Version.LessThan(IptablesRestoreLocking)
}

var (
	// IptablesRestoreLocking is the version where locking and -w is added to iptables-restore
	IptablesRestoreLocking = utilversion.MustParseGeneric("1.6.2")
	// IptablesLockfileEnv is the version where XTABLES_LOCKFILE is added to iptables.
	IptablesLockfileEnv = utilversion.MustParseGeneric("1.8.6")
)

func shouldUseBinaryForCurrentContext(iptablesBin string) (IptablesVersion, error) {
	log := log.WithLabels("binary", iptablesBin)
	// We assume that whatever `iptablesXXX` binary you pass us also has a `iptablesXXX-save` and `iptablesXXX-restore`
	// binary - which should always be true for any valid iptables installation
	// (we use both in our iptables code later on anyway)
	//
	// We could explicitly check for all 3 every time to be sure, but that's likely not necessary,
	// if we find one unless the host OS is badly broken we will find the others.
	iptablesSaveBin := fmt.Sprintf("%s-save", iptablesBin)
	iptablesRestoreBin := fmt.Sprintf("%s-restore", iptablesBin)
	var parsedVer *utilversion.Version
	var isNft bool
	// does the "xx-save" binary exist?
	rulesDump, binExistsErr := exec.Command(iptablesSaveBin).CombinedOutput()
	if binExistsErr != nil {
		return IptablesVersion{}, fmt.Errorf("failed to execute %s: %w %v", iptablesSaveBin, binExistsErr, string(rulesDump))
	}

	// Binary is there, so try to parse version
	verCmd := exec.Command(iptablesSaveBin, "--version")
	// shockingly, `iptables-save` returns 0 if you pass it an unrecognized/bad option, so
	// `os/exec` will return a *nil* error, even if the command fails. So, we must slurp stderr, and check it to
	// see if the command *actually* failed due to not recognizing the version flag.
	var verStdOut bytes.Buffer
	var verStdErr bytes.Buffer
	verCmd.Stdout = &verStdOut
	verCmd.Stderr = &verStdErr

	verExec := verCmd.Run()
	if verExec == nil && !strings.Contains(verStdErr.String(), "unrecognized option") {
		var parseErr error
		// we found the binary - extract the version, then try to detect if rules already exist for that variant
		parsedVer, parseErr = parseIptablesVer(verStdOut.String())
		if parseErr != nil {
			return IptablesVersion{}, fmt.Errorf("iptables version %q is not a valid version string: %v", verStdOut.Bytes(), parseErr)
		}
		// Legacy will have no marking or 'legacy', so just look for nf_tables
		isNft = strings.Contains(verStdOut.String(), "nf_tables")
	} else {
		log.Warnf("found iptables binary %s, but it does not appear to support the '--version' flag, assuming very old legacy version", iptablesSaveBin)
		// Some really old iptables-legacy-save versions (1.6.1, ubuntu bionic) don't support any arguments at all, including `--version`
		// So if we get here, we found `iptables-save` in PATH, but it's too outdated to understand `--version`.
		//
		// We can eventually remove this.
		//
		// So assume it's legacy/an unknown version, but assume we can use it since it's in PATH
		parsedVer = utilversion.MustParseGeneric("0.0.0")
		isNft = false
	}

	// `filter` should ALWAYS exist if kernel support for this version is locally present,
	// so try to add a no-op rule to that table, and make sure it doesn't fail - if it does, we can't use this version
	// as the host is missing kernel support for it.
	// (255 is a nonexistent protocol number, IANA reserved, see `cat /etc/protocols`, so this rule will never match anything)
	testCmd := exec.Command(iptablesBin, testRuleAdd...)

	var testStdErr bytes.Buffer
	testCmd.Stderr = &testStdErr
	testRes := testCmd.Run()
	// If we can't add a rule to the basic `filter` table, we can't use this binary - bail out.
	// Otherwise, delete the no-op rule and carry on with other checks
	if testRes != nil || strings.Contains(testStdErr.String(), "does not exist") {
		return IptablesVersion{}, fmt.Errorf("iptables binary %s has no loaded kernel support and cannot be used, err: %v out: %s",
			iptablesBin, testRes, testStdErr.String())
	}

	testRuleDel := append(make([]string, 0, len(testRuleAdd)), testRuleAdd...)
	testRuleDel[2] = "-D"

	testCmd = exec.Command(iptablesBin, testRuleDel...)
	_ = testCmd.Run()

	// if binary seems to exist, check the dump of rules in our netns, and see if any rules exist there
	// Note that this is highly dependent on context.
	// new pod netns? probably no rules. Hostnetns? probably rules
	// So this is mostly just a "hint"/heuristic as to which version we should be using, if more than one binary is present.
	// `xx-save` should return _no_ output (0 lines) if no rules are defined in this netns for that binary variant.
	// `xx-save` should return at least 3 output lines if at least one rule is defined in this netns for that binary variant.
	existingRules := false
	if strings.Count(string(rulesDump), "\n") >= 3 {
		existingRules = true
		log.Debugf("found existing rules for %s", iptablesSaveBin)
	}
	return IptablesVersion{
		DetectedBinary:        iptablesBin,
		DetectedSaveBinary:    iptablesSaveBin,
		DetectedRestoreBinary: iptablesRestoreBin,
		Version:               parsedVer,
		Legacy:                !isNft,
		ExistingRules:         existingRules,
	}, nil
}

// runInSandbox builds a lightweight sandbox ("container") to build a suitable environment to run iptables commands in.
// This is used in CNI, where commands are executed from the host but from within the container network namespace.
// This puts us in somewhat unconventionally territory.
func runInSandbox(lockFile string, f func() error) error {
	chErr := make(chan error, 1)
	n, nerr := netns.GetCurrentNS()
	if nerr != nil {
		return fmt.Errorf("failed to get current namespace: %v", nerr)
	}
	// setupSandbox builds the sandbox.
	setupSandbox := func() error {
		// First, unshare the mount namespace. This allows us to create custom mounts without impacting the host
		if err := unix.Unshare(unix.CLONE_NEWNS); err != nil {
			return fmt.Errorf("failed to unshare to new mount namespace: %v", err)
		}
		if err := n.Set(); err != nil {
			return fmt.Errorf("failed to reset network namespace: %v", err)
		}
		// Remount / as a private mount so that our mounts do not impact outside the namespace
		// (see https://unix.stackexchange.com/questions/246312/why-is-my-bind-mount-visible-outside-its-mount-namespace).
		if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
			return fmt.Errorf("failed to remount /: %v", err)
		}
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
		return nil
	}

	executed := false
	// Once we call unshare(CLONE_NEWNS), we cannot undo it explicitly. Instead, we need to unshare on a specific thread,
	// then kill that thread when we are done (or rather, let Go runtime kill the thread).
	// Unfortunately, making a new thread breaks us out of the network namespace we entered previously, so we need to restore that as well
	go func() {
		chErr <- func() error {
			// We now have exclusive access to this thread. Once the goroutine exits without calling UnlockOSThread, the go runtime will kill the thread for us
			// Warning: Do not call UnlockOSThread! Notably, netns.Do does call this.
			runtime.LockOSThread()
			if err := setupSandbox(); err != nil {
				return err
			}
			// Mark we have actually run the command. This lets us distinguish from a failure in setupSandbox() vs f()
			executed = true
			return f()
		}()
	}()
	err := <-chErr
	if err != nil && !executed {
		// We failed to setup the environment. Now we go into best effort mode.
		// Users running into this may have IPTables lock used unexpectedly or make unexpected NSS calls.
		// This is to support environments with restrictive access (from SELinux, but possibly others) that block these calls
		// See https://github.com/istio/istio/issues/48746
		log.Warnf("failed to setup execution environment, attempting to continue anyways: %v", err)
		// Try to execute as-is
		return f()
	}
	// Otherwise, we did execute; return the error from that execution.
	return err
}

func mount(src, dst string) error {
	return syscall.Mount(src, dst, "", syscall.MS_BIND|syscall.MS_RDONLY, "")
}

func (r *RealDependencies) executeXTables(log *log.Scope, cmd constants.IptablesCmd, iptVer *IptablesVersion,
	silenceErrors bool, stdin io.ReadSeeker, args ...string,
) (*bytes.Buffer, error) {
	mode := "without lock"
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	cmdBin := iptVer.CmdToString(cmd)
	if cmdBin == "" {
		return stdout, fmt.Errorf("called without iptables binary, cannot execute!: %+v", iptVer)
	}
	var c *exec.Cmd
	needLock := iptVer.IsWriteCmd(cmd) && !iptVer.NoLocks()
	run := func(c *exec.Cmd) error {
		return c.Run()
	}
	if needLock {
		// For _any_ mode where we need a lock (sandboxed or not)
		// use the wait flag. In sandbox mode we use the container's netns itself
		// as the lockfile, if one is needed, to avoid lock contention 99% of the time.
		// But a container netns is just a file, and like any Linux file,
		// we can't guarantee no other process has it locked.
		args = append(args, "--wait=30")
	}

	if r.UsePodScopedXtablesLock {
		c = exec.Command(cmdBin, args...)
		// In CNI, we are running the pod network namespace, but the host filesystem, so we need to do some tricks
		// Call our binary again, but with <original binary> "unshare (subcommand to trigger mounts)" --lock-file=<network namespace> <original command...>
		// We do not shell out and call `mount` since this and sh are not available on all systems
		var lockFile string
		if needLock {
			if iptVer.Version.LessThan(IptablesLockfileEnv) {
				mode = "sandboxed local lock by mount and nss"
				lockFile = r.NetworkNamespace
			} else {
				mode = "sandboxed local lock by env and nss"
				c.Env = append(c.Env, "XTABLES_LOCKFILE="+r.NetworkNamespace)
			}
		} else {
			mode = "sandboxed without lock"
		}

		run = func(c *exec.Cmd) error {
			return runInSandbox(lockFile, func() error {
				return c.Run()
			})
		}
	} else {
		if needLock {
			c = exec.Command(cmdBin, args...)
			log.Debugf("running with lock")
			mode = "with global lock"
		} else {
			// No locking supported/needed, just run as is. Nothing special
			c = exec.Command(cmdBin, args...)
		}
	}
	log.Debugf("Running command (%s): %s %s", mode, cmdBin, strings.Join(args, " "))

	c.Stdout = stdout
	c.Stderr = stderr
	c.Stdin = stdin
	err := run(c)
	if len(stdout.String()) != 0 {
		log.Debugf("Command output: \n%v", stdout.String())
	}

	stderrStr := stderr.String()

	if err != nil {
		// Transform to xtables-specific error messages
		transformedErr := transformToXTablesErrorMessage(stderrStr, err)

		if !silenceErrors {
			log.Errorf("Command error: %v", transformedErr)
		} else {
			// Log ignored errors for debugging purposes
			log.Debugf("Ignoring iptables command error: %v", transformedErr)
		}
		err = errors.Join(err, errors.New(stderrStr))
	} else if len(stderrStr) > 0 {
		log.Debugf("Command stderr output: %s", stderrStr)
	}

	return stdout, err
}
