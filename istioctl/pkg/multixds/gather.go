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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/types/known/anypb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/xds"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/kube"
	istioversion "istio.io/istio/pkg/version"
)

const (
	// Service account to create tokens in
	tokenServiceAccount = "default"

	proxyNotConnectedMessage = "Proxy not connected to this Pilot instance. It may be connected to another instance."
)

type ControlPlaneNotFoundError struct {
	Namespace string
}

func (c ControlPlaneNotFoundError) Error() string {
	return fmt.Sprintf("no running Istio pods in %q", c.Namespace)
}

var _ error = ControlPlaneNotFoundError{}

type Options struct {
	// MessageWriter is a writer for displaying messages to users.
	MessageWriter io.Writer
}

var DefaultOptions = Options{
	MessageWriter: os.Stdout,
}

// RequestAndProcessXds merges XDS responses from 1 central or 1..N K8s cluster-based XDS servers
// Deprecated This method makes multiple responses appear to come from a single control plane;
// consider using AllRequestAndProcessXds or FirstRequestAndProcessXds
// nolint: lll
func RequestAndProcessXds(dr *discovery.DiscoveryRequest, centralOpts clioptions.CentralControlPlaneOptions, istioNamespace string, kubeClient kube.CLIClient) (*discovery.DiscoveryResponse, error) {
	responses, err := MultiRequestAndProcessXds(true, dr, centralOpts, istioNamespace,
		istioNamespace, tokenServiceAccount, kubeClient, DefaultOptions)
	if err != nil {
		return nil, err
	}
	return mergeShards(responses)
}

var GetXdsResponse = xds.GetXdsResponse

// nolint: lll
func queryEachShard(all bool, dr *discovery.DiscoveryRequest, istioNamespace string, kubeClient kube.CLIClient, centralOpts clioptions.CentralControlPlaneOptions) ([]*discovery.DiscoveryResponse, error) {
	labelSelector := centralOpts.XdsPodLabel
	if labelSelector == "" {
		labelSelector = "app=istiod"
	}
	pods, err := kubeClient.GetIstioPods(context.TODO(), istioNamespace, metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: kube.RunningStatus,
	})
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, ControlPlaneNotFoundError{istioNamespace}
	}

	responses := []*discovery.DiscoveryResponse{}
	xdsOpts := clioptions.CentralControlPlaneOptions{
		XDSSAN:  makeSan(istioNamespace, kubeClient.Revision()),
		CertDir: centralOpts.CertDir,
		Timeout: centralOpts.Timeout,
	}
	dialOpts, err := xds.DialOptions(xdsOpts, istioNamespace, tokenServiceAccount, kubeClient)
	if err != nil {
		return nil, err
	}

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
		xdsOpts.Xds = fw.Address()
		response, err := GetXdsResponse(dr, istioNamespace, tokenServiceAccount, xdsOpts, dialOpts)
		if err != nil {
			return nil, fmt.Errorf("could not get XDS from discovery pod %q: %v", pod.Name, err)
		}

		if all {
			// If we are getting response from all istiod pods, we should append all responses.
			responses = append(responses, response)
			continue
		}

		// If we are not getting response from all istiod pods, and this response is not from the last one istiod pod,
		// and it is a response that indicates the proxy is not connected to this current istiod instance,
		// we should skip this response and try the next istiod pod.
		// This is very useful to get response from a multi replicas istiod cluster and short the time cost than `all=true`.
		if !proxyNotConnectedToThisPilotInstanceResponse(response) {
			responses = append(responses, response)
			break
		}
	}

	// If we are not getting response from all istiod pods,
	// return with the first response that contains resources.
	if len(responses) == 0 {
		responses = append(responses, &discovery.DiscoveryResponse{
			Resources: []*anypb.Any{
				{
					Value: []byte(proxyNotConnectedMessage),
				},
			},
		})
	}
	return responses, nil
}

func proxyNotConnectedToThisPilotInstanceResponse(resp *discovery.DiscoveryResponse) bool {
	if resp == nil || len(resp.Resources) != 1 {
		return false
	}
	for _, res := range resp.Resources {
		if strings.Contains(string(res.GetValue()), proxyNotConnectedMessage) {
			return true
		}
	}

	return false
}

func mergeShards(responses map[string]*discovery.DiscoveryResponse) (*discovery.DiscoveryResponse, error) {
	retval := discovery.DiscoveryResponse{}
	if len(responses) == 0 {
		return &retval, nil
	}

	for _, response := range responses {
		// Combine all the shards as one, even if that means losing information about
		// the control plane version from each shard.
		retval.ControlPlane = response.ControlPlane
		retval.Resources = append(retval.Resources, response.Resources...)
	}

	return &retval, nil
}

func makeSan(istioNamespace, revision string) string {
	if revision == "" {
		return fmt.Sprintf("istiod.%s.svc", istioNamespace)
	}
	return fmt.Sprintf("istiod-%s.%s.svc", revision, istioNamespace)
}

// AllRequestAndProcessXds returns all XDS responses from 1 central or 1..N K8s cluster-based XDS servers
// nolint: lll
func AllRequestAndProcessXds(dr *discovery.DiscoveryRequest, centralOpts clioptions.CentralControlPlaneOptions, istioNamespace string,
	ns string, serviceAccount string, kubeClient kube.CLIClient, options Options,
) (map[string]*discovery.DiscoveryResponse, error) {
	return MultiRequestAndProcessXds(true, dr, centralOpts, istioNamespace, ns, serviceAccount, kubeClient, options)
}

// FirstRequestAndProcessXds returns all XDS responses from 1 central or 1..N K8s cluster-based XDS servers,
// stopping after the first response that returns any resources.
// nolint: lll
func FirstRequestAndProcessXds(dr *discovery.DiscoveryRequest, centralOpts clioptions.CentralControlPlaneOptions, istioNamespace string,
	ns string, serviceAccount string, kubeClient kube.CLIClient, options Options,
) (map[string]*discovery.DiscoveryResponse, error) {
	return MultiRequestAndProcessXds(false, dr, centralOpts, istioNamespace, ns, serviceAccount, kubeClient, options)
}

type xdsAddr struct {
	gcpProject, host, istiod string
}

func getXdsAddressFromWebhooks(client kube.CLIClient) (*xdsAddr, error) {
	webhooks, err := client.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,!%s", label.IoIstioRev.Name, client.Revision(), label.IoIstioTag.Name),
	})
	if err != nil {
		return nil, err
	}
	for _, whc := range webhooks.Items {
		for _, wh := range whc.Webhooks {
			if wh.ClientConfig.URL != nil {
				u, err := url.Parse(*wh.ClientConfig.URL)
				if err != nil {
					return nil, fmt.Errorf("parsing webhook URL: %w", err)
				}
				if isMCPAddr(u) {
					return parseMCPAddr(u)
				}
				port := u.Port()
				if port == "" {
					port = "443" // default from Kubernetes
				}
				return &xdsAddr{host: net.JoinHostPort(u.Hostname(), port)}, nil
			}
		}
	}
	return nil, errors.New("xds address not found")
}

// nolint: lll
func MultiRequestAndProcessXds(all bool, dr *discovery.DiscoveryRequest, centralOpts clioptions.CentralControlPlaneOptions, istioNamespace string,
	ns string, serviceAccount string, kubeClient kube.CLIClient, options Options,
) (map[string]*discovery.DiscoveryResponse, error) {
	// If Central Istiod case, just call it
	if ns == "" {
		ns = istioNamespace
	}
	if ns == istioNamespace {
		serviceAccount = tokenServiceAccount
	}
	if centralOpts.Xds != "" {
		dialOpts, err := xds.DialOptions(centralOpts, ns, serviceAccount, kubeClient)
		if err != nil {
			return nil, err
		}
		response, err := xds.GetXdsResponse(dr, ns, serviceAccount, centralOpts, dialOpts)
		if err != nil {
			return nil, err
		}
		return map[string]*discovery.DiscoveryResponse{
			CpInfo(response).ID: response,
		}, nil
	}

	// Find all Istiods in revision using K8s, port-forward and call each in turn
	responses, err := queryEachShard(all, dr, istioNamespace, kubeClient, centralOpts)
	if err != nil {
		if _, ok := err.(ControlPlaneNotFoundError); ok {
			// Attempt to get the XDS address from the webhook and try again
			addr, err := getXdsAddressFromWebhooks(kubeClient)
			if err == nil {
				centralOpts.Xds = addr.host
				centralOpts.GCPProject = addr.gcpProject
				centralOpts.IstiodAddr = addr.istiod
				dialOpts, err := xds.DialOptions(centralOpts, istioNamespace, tokenServiceAccount, kubeClient)
				if err != nil {
					return nil, err
				}
				response, err := xds.GetXdsResponse(dr, istioNamespace, tokenServiceAccount, centralOpts, dialOpts)
				if err != nil {
					return nil, err
				}
				return map[string]*discovery.DiscoveryResponse{
					CpInfo(response).ID: response,
				}, nil
			}
		}
		return nil, err
	}
	return mapShards(responses)
}

func mapShards(responses []*discovery.DiscoveryResponse) (map[string]*discovery.DiscoveryResponse, error) {
	retval := map[string]*discovery.DiscoveryResponse{}

	for _, response := range responses {
		retval[CpInfo(response).ID] = response
	}

	return retval, nil
}

// CpInfo returns the Istio control plane info from JSON-encoded XDS ControlPlane Identifier
func CpInfo(xdsResponse *discovery.DiscoveryResponse) pilotxds.IstioControlPlaneInstance {
	if xdsResponse.ControlPlane == nil {
		return pilotxds.IstioControlPlaneInstance{
			Component: "MISSING",
			ID:        "MISSING",
			Info: istioversion.BuildInfo{
				Version: "MISSING CP ID",
			},
		}
	}

	cpID := pilotxds.IstioControlPlaneInstance{}
	err := json.Unmarshal([]byte(xdsResponse.ControlPlane.Identifier), &cpID)
	if err != nil {
		return pilotxds.IstioControlPlaneInstance{
			Component: "INVALID",
			ID:        "INVALID",
			Info: istioversion.BuildInfo{
				Version: "INVALID CP ID",
			},
		}
	}
	return cpID
}
