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

	envoy_corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/operator/cmd/mesh"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/proxy"
	istioVersion "istio.io/pkg/version"
)

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
		return proxy.GetProxyInfo(kubeconfig, configContext, opts.Revision, istioNamespace)
	}
}

// xdsVersionCommand gets the Control Plane and Sidecar versions via XDS
func xdsVersionCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var centralOpts clioptions.CentralControlPlaneOptions
	var xdsResponses *xdsapi.DiscoveryResponse
	versionCmd := istioVersion.CobraCommandWithOptions(istioVersion.CobraOptions{
		GetRemoteVersion: xdsRemoteVersionWrapper(&opts, &centralOpts, &xdsResponses),
		GetProxyVersions: xdsProxyVersionWrapper(&xdsResponses),
	})
	opts.AttachControlPlaneFlags(versionCmd)
	centralOpts.AttachControlPlaneFlags(versionCmd)
	versionCmd.Args = func(c *cobra.Command, args []string) error {
		if err := cobra.NoArgs(c, args); err != nil {
			return err
		}
		if err := centralOpts.ValidateControlPlaneFlags(); err != nil {
			return err
		}
		return nil
	}
	versionCmd.Example = `# Retrieve version information directly from XDS, without security
istioctl x version --xds-address localhost:15012

# Retrieve version information directly from XDS, with security
# (the certificates must be retrieved before this step)
istioctl x version --xds-address localhost:15010 --cert-dir ~/.istio-certs

# Retrieve version information via XDS from all Istio pods in a Kubernetes cluster
# (without security)
istioctl x version --xds-port 15010

# Retrieve version information via XDS from all Istio pods in a Kubernetes cluster
# (the certificates must be retrieved before this step)
istioctl x version --cert-dir ~/.istio-certs

# Retrieve version information via XDS from default control plane Istio pods
# in a Kubernetes cluster, without security
istioctl x version --xds-label istio.io/rev=default --xds-port 15010
`

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

// xdsRemoteVersionWrapper uses outXDS to share the XDS response with xdsProxyVersionWrapper.
// (Screwy API on istioVersion.CobraCommandWithOptions)
// nolint: lll
func xdsRemoteVersionWrapper(opts *clioptions.ControlPlaneOptions, centralOpts *clioptions.CentralControlPlaneOptions, outXDS **xdsapi.DiscoveryResponse) func() (*istioVersion.MeshInfo, error) {
	return func() (*istioVersion.MeshInfo, error) {
		xdsRequest := xdsapi.DiscoveryRequest{
			Node: &envoy_corev3.Node{
				Id: "sidecar~0.0.0.0~debug~cluster.local",
			},
			TypeUrl: "istio.io/connections",
		}
		kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
		if err != nil {
			return nil, err
		}
		xdsResponse, err := multixds.RequestAndProcessXds(&xdsRequest, centralOpts, istioNamespace, kubeClient)
		if err != nil {
			return nil, err
		}
		*outXDS = xdsResponse
		if xdsResponse.ControlPlane == nil {
			return &istioVersion.MeshInfo{
				istioVersion.ServerInfo{
					Component: "MISSING CP ID",
					Info: istioVersion.BuildInfo{
						Version: "MISSING CP ID",
					},
				},
			}, nil
		}
		cpID := xds.IstioControlPlaneInstance{}
		err = json.Unmarshal([]byte(xdsResponse.ControlPlane.Identifier), &cpID)
		if err != nil {
			return nil, fmt.Errorf("could not parse CP Identifier: %w", err)
		}
		return &istioVersion.MeshInfo{
			istioVersion.ServerInfo{
				Component: cpID.Component,
				Info:      cpID.Info,
			},
		}, nil
	}
}

func xdsProxyVersionWrapper(xdsResponse **xdsapi.DiscoveryResponse) func() (*[]istioVersion.ProxyInfo, error) {
	return func() (*[]istioVersion.ProxyInfo, error) {
		pi := []istioVersion.ProxyInfo{}
		for _, resource := range (*xdsResponse).Resources {
			switch resource.TypeUrl {
			case "type.googleapis.com/envoy.config.core.v3.Node":
				node := envoy_corev3.Node{}
				err := ptypes.UnmarshalAny(resource, &node)
				if err != nil {
					return nil, fmt.Errorf("could not unmarshal Node: %w", err)
				}
				meta, err := model.ParseMetadata(node.Metadata)
				if err != nil || meta.ProxyConfig == nil {
					// Skip non-sidecars (e.g. istioctl queries)
					continue
				}
				pi = append(pi, istioVersion.ProxyInfo{
					ID:           node.Id,
					IstioVersion: getIstioVersionFromXdsMetadata(node.Metadata),
				})
			default:
				return nil, fmt.Errorf("unexpected resource type %q", resource.TypeUrl)
			}
		}
		return &pi, nil
	}
}

func getIstioVersionFromXdsMetadata(metadata *structpb.Struct) string {
	meta, err := model.ParseMetadata(metadata)
	if err != nil {
		return "unknown sidecar version"
	}
	return meta.IstioVersion
}
