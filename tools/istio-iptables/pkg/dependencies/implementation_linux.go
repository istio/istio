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
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	utilversion "k8s.io/apimachinery/pkg/util/version"

	"istio.io/istio/pkg/log"
)

// NoLocks returns true if this version does not use or support locks
func (v IptablesVersion) NoLocks() bool {
	// nf_tables does not use locks
	// legacy added locks in 1.6.2
	return !v.legacy || v.version.LessThan(IptablesRestoreLocking)
}

var (
	// IptablesRestoreLocking is the version where locking and -w is added to iptables-restore
	IptablesRestoreLocking = utilversion.MustParseGeneric("1.6.2")
	// IptablesLockfileEnv is the version where XTABLES_LOCKFILE is added to iptables.
	IptablesLockfileEnv = utilversion.MustParseGeneric("1.8.6")
)

func doUnshare(f func() error) error {
	chErr := make(chan error, 1)
	go func() {
		chErr <- func() error {
			runtime.LockOSThread()
			if err := unix.Unshare(unix.CLONE_NEWNS); err != nil {
				return err
			}
			if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
				return &os.PathError{Op: "mount", Path: "/", Err: err}
			}
			return f()
		}()
	}()
	return <-chErr
}

func mount(src, dst string) error {
	return syscall.Mount(src, dst, "", syscall.MS_BIND|syscall.MS_RDONLY, "")
}

func (r *RealDependencies) executeXTables(cmd string, ignoreErrors bool, stdin io.ReadSeeker, args ...string) error {
	mode := "without lock"
	var c *exec.Cmd
	_, isWriteCommand := XTablesWriteCmds[cmd]
	needLock := isWriteCommand && !r.IptablesVersion.NoLocks()
	run := func(c *exec.Cmd) error {
		return c.Run()
	}
	if r.CNIMode {
		c = exec.Command(cmd, args...)
		// In CNI, we are running the pod network namespace, but the host filesystem, so we need to do some tricks
		// Call our binary again, but with <original binary> "unshare (subcommand to trigger mounts)" --lock-file=<network namespace> <original command...>
		// We do not shell out and call `mount` since this and sh are not available on all systems
		var lockFile string
		if needLock {
			lockFile = r.NetworkNamespace
			mode = "without lock or nss"
		} else {
			mode = "without nss"
		}

		run = func(c *exec.Cmd) error {
			return doUnshare(func() error {
					if err := mount("/tmp/a", "/tmp/b"); err != nil {
						return fmt.Errorf("bind mount of %q failed: %v", lockFile, err)
					}
				if lockFile != "" {
					if err := mount(lockFile, "/run/xtables.lock"); err != nil {
						return fmt.Errorf("bind mount of %q failed: %v", lockFile, err)
					}
				}
				if err := mount("/dev/null", "/etc/nsswitch.conf"); err != nil {
					log.Warnf("bind mount to %q failed: %v", "/etc/nsswitch.conf", err)
					//return fmt.Errorf("bind mount to %q failed: %v", "/etc/nsswitch.conf", err)
				}
				return c.Run()
			})
		}
	} else {
		if needLock {
			// We want the lock. Wait up to 30s for it.
			args = append(args, "--wait=30")
			c = exec.Command(cmd, args...)
			log.Debugf("running with lock")
			mode = "with wait lock"
		} else {
			// No locking supported/needed, just run as is. Nothing special
			c = exec.Command(cmd, args...)
		}
	}

	log.Infof("Running command (%s): %s %s", mode, cmd, strings.Join(args, " "))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	c.Stdout = stdout
	c.Stderr = stderr
	c.Stdin = stdin
	err := run(c)
	if len(stdout.String()) != 0 {
		log.Infof("Command output: \n%v", stdout.String())
	}

	// TODO Check naming and redirection logic
	if (err != nil || len(stderr.String()) != 0) && !ignoreErrors {
		stderrStr := stderr.String()

		// Transform to xtables-specific error messages with more useful and actionable hints.
		if err != nil {
			stderrStr = transformToXTablesErrorMessage(stderrStr, err)
		}

		log.Errorf("Command error output: %v", stderrStr)
	}

	return err
}
