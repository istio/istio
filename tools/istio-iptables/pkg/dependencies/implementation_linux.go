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
	"os/exec"
	"strings"
	"syscall"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/spf13/viper"
	"golang.org/x/sys/unix"
	utilversion "k8s.io/apimachinery/pkg/util/version"

	"istio.io/istio/pkg/log"
)

func (r *RealDependencies) execute(cmd string, ignoreErrors bool, stdin io.Reader, args ...string) error {
	log.Infof("Running command: %s %s", cmd, strings.Join(args, " "))

	externalCommand := exec.Command(cmd, args...)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	externalCommand.Stdout = stdout
	externalCommand.Stderr = stderr
	externalCommand.Stdin = stdin

	// Grab all viper config and propagate it as environment variables to the child process
	repl := strings.NewReplacer("-", "_")
	for _, k := range viper.AllKeys() {
		v := viper.Get(k)
		if v == nil {
			continue
		}
		externalCommand.Env = append(externalCommand.Env, fmt.Sprintf("%s=%v", strings.ToUpper(repl.Replace(k)), v))
	}
	err := r.runCommand(externalCommand)
	if len(stdout.String()) != 0 {
		log.Infof("Command output: \n%v", stdout.String())
	}

	if !ignoreErrors && len(stderr.Bytes()) != 0 {
		log.Errorf("Command error output: \n%v", stderr.String())
	}

	return err
}

func (r *RealDependencies) runCommand(c *exec.Cmd) error {
	if r.CNIMode {
		n, err := ns.GetNS(r.NetworkNamespace)
		if err != nil {
			return err
		}
		defer n.Close()

		return n.Do(func(ns.NetNS) error {
			return c.Run()
		})
	}
	return c.Run()
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
	_, needLock := XTablesWriteCmds[cmd]
	if !needLock || r.IptablesVersion.NoLocks() {
		// No locking supported/needed, just run as is. Nothing special
		c = exec.Command(cmd, args...)
	} else {
		if r.CNIMode {
			// In CNI, we are running the pod network namespace, but the host filesystem. Locking the host is both useless and harmful,
			// as it opens the risk of lock contention with other node actors (such as kube-proxy), and isn't actually needed at all.
			// In both cases we are setting the lockfile to `r.NetworkNamespace`.
			// * /dev/null looks like a good option, but actually doesn't work (it will ensure only one actor can access it)
			// * `mktemp` works, but it is slightly harder to deal with cleanup and in some platforms we may not have write access.
			if r.IptablesVersion.version.LessThan(IptablesLockfileEnv) {
				// Older iptables cannot turn off the lock explicitly, so we hack around it...
				// Overwrite the lock file with /dev/null
				// cmd is repeated twice as the first 'cmd' instance becomes $0
				args := append([]string{"-c", fmt.Sprintf("mount --bind %s /run/xtables.lock; exec $@", r.NetworkNamespace), cmd, cmd}, args...)
				c = exec.Command("sh", args...)
				// Run in a new mount namespace so our mount doesn't impact any other processes.
				c.SysProcAttr = &syscall.SysProcAttr{Unshareflags: unix.CLONE_NEWNS}
				mode = "without lock by mount"
			} else {
				// Available since iptables 1.8.6+, just point to a different file directly
				c = exec.Command(cmd, args...)
				c.Env = append(c.Env, "XTABLES_LOCKFILE="+r.NetworkNamespace)
				mode = "without lock by environment"
			}
		} else {
			// We want the lock. Wait up to 30s for it.
			args = append(args, "--wait=30")
			c = exec.Command(cmd, args...)
			log.Debugf("running with lock")
			mode = "with wait lock"
		}
	}

	log.Infof("Running command (%s): %s %s", mode, cmd, strings.Join(args, " "))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	c.Stdout = stdout
	c.Stderr = stderr
	c.Stdin = stdin
	err := r.runCommand(c)
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
