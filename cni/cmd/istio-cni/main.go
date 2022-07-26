// Copyright 2018 Istio Authors
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
	"fmt"
	"os"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"

	"istio.io/istio/cni/pkg/plugin"
	"istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	"istio.io/pkg/log"
	istioversion "istio.io/pkg/version"
)

func main() {
	if err := log.Configure(plugin.GetLoggingOptions("")); err != nil {
		os.Exit(1)
	}
	defer func() {
		// Log sync will send logs to install-cni container via UDS.
		// We don't need a timeout here because underlying the log pkg already handles it.
		// this may fail, but it should be safe to ignore according
		// to https://github.com/uber-go/zap/issues/328
		_ = log.Sync()
	}()

	// configure-routes allows setting up the iproute2 configuration.
	// This is an old workaround and kept in place to preserve old behavior.
	// It is called standalone when HostNSEnterExec=true.
	// Default behavior is to use go netns, which is not in need for this:

	// Older versions of Go < 1.10 cannot change network namespaces safely within a go program
	// (see https://www.weave.works/blog/linux-namespaces-and-go-don-t-mix).
	// As a result, the flow is:
	// * CNI plugin is called with no args, skipping this section.
	// * CNI code invokes iptables code with CNIMode=true. This in turn runs 'nsenter -- istio-cni configure-routes'
	if len(os.Args) > 1 && os.Args[1] == constants.CommandConfigureRoutes {
		if err := cmd.GetRouteCommand().Execute(); err != nil {
			log.Errorf("failed to configure routes: %v", err)
			os.Exit(1)
		}
		return
	}

	// TODO: implement plugin version
	skel.PluginMain(plugin.CmdAdd, plugin.CmdCheck, plugin.CmdDelete, version.All,
		fmt.Sprintf("CNI plugin istio-cni %v", istioversion.Info.Version))
}
