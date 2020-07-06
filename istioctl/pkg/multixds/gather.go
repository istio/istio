// Copyright Istio Authors.
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

package multixds

// multixds knows how to target either central Istiod or all the Istiod pods on a cluster.

import (
	"context"
	"fmt"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/xds"
	"istio.io/istio/pkg/kube"
)

// RequestAndProcessXds merges XDS responses from 1 central or 1..N local XDS servers
// nolint: lll
func RequestAndProcessXds(dr *xdsapi.DiscoveryRequest, centralOpts *clioptions.CentralControlPlaneOptions, istioNamespace string, kubeClient kube.ExtendedClient) (*xdsapi.DiscoveryResponse, error) {

	// If Central Istiod case, just call it
	if centralOpts.Xds != "" {
		return xds.GetXdsResponse(dr, centralOpts)
	}

	// Self-administered case.  Find all Istiods in revision using K8s, port-forward and call each in turn
	responses, err := queryEachShard(dr, istioNamespace, kubeClient, centralOpts)
	if err != nil {
		return nil, err
	}
	return mergeShards(responses)
}

// nolint: lll
func queryEachShard(dr *xdsapi.DiscoveryRequest, istioNamespace string, kubeClient kube.ExtendedClient, centralOpts *clioptions.CentralControlPlaneOptions) ([]*xdsapi.DiscoveryResponse, error) {
	labelSelector := centralOpts.XdsPodLabel
	if labelSelector == "" {
		labelSelector = "istio=pilot"
	}
	pods, err := kubeClient.GetIstioPods(context.TODO(), istioNamespace, map[string]string{
		"labelSelector": labelSelector,
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no running Istio pods in %q", istioNamespace)
	}

	responses := []*xdsapi.DiscoveryResponse{}
	for _, pod := range pods {
		fw, err := kubeClient.NewPortForwarder(pod.Name, pod.Namespace, "localhost", 0, centralOpts.XdsPodPort)
		if err != nil {
			return nil, err
		}
		err = fw.Start()
		if err != nil {
			return nil, err
		}
		defer fw.Close()
		response, err := xds.GetXdsResponse(dr, &clioptions.CentralControlPlaneOptions{
			Xds:     fw.Address(),
			CertDir: centralOpts.CertDir,
			Timeout: centralOpts.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("could not get XDS from discovery pod %q: %v", pod.Name, err)
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func mergeShards(responses []*xdsapi.DiscoveryResponse) (*xdsapi.DiscoveryResponse, error) {
	retval := xdsapi.DiscoveryResponse{}
	if len(responses) == 0 {
		return &retval, nil
	}

	// Combine all the shards as one, even if that means losing information about
	// the control plane version from each shard.
	retval.ControlPlane = responses[0].ControlPlane

	for _, response := range responses {
		retval.Resources = append(retval.Resources, response.Resources...)
	}

	return &retval, nil
}
