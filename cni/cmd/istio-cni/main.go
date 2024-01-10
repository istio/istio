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

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"

	"istio.io/istio/cni/pkg/mount"
	"istio.io/istio/cni/pkg/plugin"
	"istio.io/istio/pkg/log"
	istioversion "istio.io/istio/pkg/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "unshare" {
		runUnshare()
		return
	}
	if err := runPlugin(); err != nil {
		os.Exit(1)
	}
}

func runPlugin() error {
	if err := log.Configure(plugin.GetLoggingOptions("")); err != nil {
		return err
	}
	defer func() {
		// Log sync will send logs to install-cni container via UDS.
		// We don't need a timeout here because underlying the log pkg already handles it.
		// this may fail, but it should be safe to ignore according
		// to https://github.com/uber-go/zap/issues/328
		_ = log.Sync()
	}()

	// TODO: implement plugin version
	err := skel.PluginMainWithError(plugin.CmdAdd, plugin.CmdCheck, plugin.CmdDelete, version.All,
		fmt.Sprintf("CNI plugin istio-cni %v", istioversion.Info.Version))
	if err != nil {
		log.Errorf("istio-cni failed with: %v", err)
		if err := err.Print(); err != nil {
			log.Errorf("istio-cni failed to write error JSON to stdout: %v", err)
		}

		return err
	}

	return nil
}

// runUnshare triggers subcommand of istio-cni. This is used to invoke ourself to avoid dependencies on the node (the *one*
// thing we know must exist on the node is this binary, since its called from ourselves!).
// This will prepare the mount namespace(s) before executing a command.
func runUnshare() {
	fs := flag.NewFlagSet("unshare", flag.ExitOnError)
	netns := fs.String("lock-file", "", "file to override lock")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(fs.Args()) < 1 {
		fmt.Fprint(os.Stderr, "usage: unshare --lock-file=file -- <command> [args...]\n")
		os.Exit(1)
	}
	// Skip first arg which is "unshare"
	if err := mount.RunSandboxed(*netns, fs.Args()); err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
}
