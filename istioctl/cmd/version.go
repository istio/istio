// Copyright 2019 Istio Authors
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

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"

	istioVersion "istio.io/pkg/version"
)

type sidecarSyncStatus struct {
	// nolint: structcheck, unused
	pilot string
	v2.SyncStatus
}

func newVersionCommand() *cobra.Command {
	versionCmd := istioVersion.CobraCommandWithOptions(istioVersion.CobraOptions{
		GetRemoteVersion: getRemoteInfo,
		GetProxyVersions: getProxyInfo,
	})

	versionCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "short" {
			err := flag.Value.Set("true")
			if err != nil {
				fmt.Fprint(os.Stdout, fmt.Sprintf("set flag %q as true failed due to error %v", flag.Name, err))
			}
		}
		if flag.Name == "remote" {
			err := flag.Value.Set("true")
			if err != nil {
				fmt.Fprint(os.Stdout, fmt.Sprintf("set flag %q as true failed due to error %v", flag.Name, err))
			}
		}
	})
	return versionCmd
}

func getRemoteInfo() (*istioVersion.MeshInfo, error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	return kubeClient.GetIstioVersions(istioNamespace)
}

func getProxyInfo() (*[]istioVersion.ProxyInfo, error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	// Ask Pilot for the Envoy sidecar sync status, which includes the sidecar version info
	allSyncz, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", "/debug/syncz", nil)
	if err != nil {
		return nil, err
	}

	pi := []istioVersion.ProxyInfo{}
	for _, syncz := range allSyncz {
		var sss []*sidecarSyncStatus
		err = json.Unmarshal(syncz, &sss)
		if err != nil {
			return nil, err
		}

		for _, ss := range sss {
			pi = append(pi, istioVersion.ProxyInfo{
				ID:           ss.ProxyID,
				IstioVersion: ss.SyncStatus.IstioVersion,
			})
		}
	}

	return &pi, nil
}
