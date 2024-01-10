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
	"io"
	"os"
	"os/exec"
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

func (r *RealDependencies) executeXTables(cmd string, ignoreErrors bool, stdin io.ReadSeeker, args ...string) error {
	mode := "without lock"
	var c *exec.Cmd
	_, isWriteCommand := XTablesWriteCmds[cmd]
	needLock := isWriteCommand && !r.IptablesVersion.NoLocks()
	if r.CNIMode {
		// In CNI, we are running the pod network namespace, but the host filesystem, so we need to do some tricks
		// Call our binary again, but with <original binary> "unshare (subcommand to trigger mounts)" --lock-file=<network namespace> <original command...>
		// We do not shell out and call `mount` since this and sh are not available on all systems
		var args []string
		if needLock {
			args = append([]string{"unshare", "--lock-file=" + r.NetworkNamespace, cmd}, args...)
			mode = "without lock or nss"
		} else {
			// We still need unshare for the nsswitch case.
			args = append([]string{"unshare", cmd}, args...)
			mode = "without nss"
		}

		c = exec.Command(os.Args[0], args...)
		// Run in a new mount namespace so our mount doesn't impact any other processes.
		c.SysProcAttr = &syscall.SysProcAttr{Unshareflags: unix.CLONE_NEWNS}
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
	err := c.Run()
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
