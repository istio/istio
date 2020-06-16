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

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/operator/cmd/mesh"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"

	istioVersion "istio.io/pkg/version"
)

type sidecarSyncStatus struct {
	// nolint: structcheck, unused
	pilot string
	v2.SyncStatus
}

func newVersionCommand() *cobra.Command {
	profileCmd := mesh.ProfileCmd()
	var opts clioptions.ControlPlaneOptions
	versionCmd := istioVersion.CobraCommandWithOptions(istioVersion.CobraOptions{
		GetRemoteVersion: getRemoteInfoWrapper(&profileCmd, &opts),
		GetProxyVersions: getProxyInfoWrapper(&opts),
	})
	opts.AttachControlPlaneFlags(versionCmd)

	versionCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "short" {
			err := flag.Value.Set("true")
			if err != nil {
				fmt.Fprintf(os.Stdout, "set flag %q as true failed due to error %v", flag.Name, err)
			}
		}
		if flag.Name == "remote" {
			err := flag.Value.Set("true")
			if err != nil {
				fmt.Fprintf(os.Stdout, "set flag %q as true failed due to error %v", flag.Name, err)
			}
		}
	})
	return versionCmd
}

func getRemoteInfo(opts clioptions.ControlPlaneOptions) (*istioVersion.MeshInfo, error) {
	kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
	if err != nil {
		return nil, err
	}

	return kubeClient.GetIstioVersions(context.TODO(), istioNamespace)
}

func getRemoteInfoWrapper(pc **cobra.Command, opts *clioptions.ControlPlaneOptions) func() (*istioVersion.MeshInfo, error) {
	return func() (*istioVersion.MeshInfo, error) {
		remInfo, err := getRemoteInfo(*opts)
		if err != nil {
			fmt.Fprintf((*pc).OutOrStdout(), "%v\n", err)
			// Return nil so that the client version is printed
			return nil, nil
		}
		if remInfo == nil {
			fmt.Fprintf((*pc).OutOrStdout(), "Istio is not present in the cluster with namespace %q\n", istioNamespace)
		}
		return remInfo, err
	}
}

func getProxyInfoWrapper(opts *clioptions.ControlPlaneOptions) func() (*[]istioVersion.ProxyInfo, error) {
	return func() (*[]istioVersion.ProxyInfo, error) {
		return getProxyInfo(opts)
	}
}

func getProxyInfo(opts *clioptions.ControlPlaneOptions) (*[]istioVersion.ProxyInfo, error) {
	kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
	if err != nil {
		return nil, err
	}

	// Ask Pilot for the Envoy sidecar sync status, which includes the sidecar version info
	allSyncz, err := kubeClient.AllDiscoveryDo(context.TODO(), istioNamespace, "/debug/syncz")
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
