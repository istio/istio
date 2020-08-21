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
	"io/ioutil"
	"os"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/xds"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/kube"
	istioversion "istio.io/pkg/version"
)

type istioctlTokenSupplier struct {
	opts       *clioptions.CentralControlPlaneOptions
	kubeClient kube.ExtendedClient

	// Send messages to user on this channel, typically cmd.OutOrStderr()
	userWriter io.Writer
}

var _ adsc.TokenReader = &istioctlTokenSupplier{}

const (
	// defaultExpirationSeconds is how long-lived a token to request
	defaultExpirationSeconds = 60 * 60
)

// RequestAndProcessXds merges XDS responses from 1 central or 1..N K8s cluster-based XDS servers
// Deprecated This method makes multiple responses appear to come from a single control plane;
// consider using AllRequestAndProcessXds or FirstRequestAndProcessXds
// nolint: lll
func RequestAndProcessXds(dr *xdsapi.DiscoveryRequest, centralOpts *clioptions.CentralControlPlaneOptions, istioNamespace string, kubeClient kube.ExtendedClient, userWriter io.Writer) (*xdsapi.DiscoveryResponse, error) {

	// If Central Istiod case, just call it
	if centralOpts.Xds != "" {
		return xds.GetXdsResponse(dr, centralOpts, useToken(centralOpts, kubeClient, userWriter))
	}

	// Self-administered case.  Find all Istiods in revision using K8s, port-forward and call each in turn
	responses, err := queryEachShard(true, dr, istioNamespace, kubeClient, centralOpts, userWriter)
	if err != nil {
		return nil, err
	}
	return mergeShards(responses)
}

// nolint: lll
func queryEachShard(all bool, dr *xdsapi.DiscoveryRequest, istioNamespace string, kubeClient kube.ExtendedClient, centralOpts *clioptions.CentralControlPlaneOptions, userWriter io.Writer) ([]*xdsapi.DiscoveryResponse, error) {
	labelSelector := centralOpts.XdsPodLabel
	if labelSelector == "" {
		labelSelector = "app=istiod"
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
		xdsOpts := clioptions.CentralControlPlaneOptions{
			Xds:     fw.Address(),
			XDSSAN:  makeSan(istioNamespace, kubeClient.Revision()),
			CertDir: centralOpts.CertDir,
			Timeout: centralOpts.Timeout,
		}
		response, err := xds.GetXdsResponse(dr, &xdsOpts, useToken(&xdsOpts, kubeClient, userWriter))
		if err != nil {
			return nil, fmt.Errorf("could not get XDS from discovery pod %q: %v", pod.Name, err)
		}
		responses = append(responses, response)
		if !all && len(responses) > 0 {
			break
		}
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

func makeSan(istioNamespace, revision string) string {
	if revision == "" {
		return fmt.Sprintf("istiod.%s.svc", istioNamespace)
	}
	return fmt.Sprintf("istiod-%s.%s.svc", revision, istioNamespace)
}

// AllRequestAndProcessXds returns all XDS responses from 1 central or 1..N K8s cluster-based XDS servers
// nolint: lll
func AllRequestAndProcessXds(dr *xdsapi.DiscoveryRequest, centralOpts *clioptions.CentralControlPlaneOptions, istioNamespace string, kubeClient kube.ExtendedClient, userWriter io.Writer) (map[string]*xdsapi.DiscoveryResponse, error) {
	return multiRequestAndProcessXds(true, dr, centralOpts, istioNamespace, kubeClient, userWriter)
}

// FirstRequestAndProcessXds returns all XDS responses from 1 central or 1..N K8s cluster-based XDS servers,
// stopping after the first response that returns any resources.
// nolint: lll
func FirstRequestAndProcessXds(dr *xdsapi.DiscoveryRequest, centralOpts *clioptions.CentralControlPlaneOptions, istioNamespace string, kubeClient kube.ExtendedClient, userWriter io.Writer) (map[string]*xdsapi.DiscoveryResponse, error) {
	return multiRequestAndProcessXds(false, dr, centralOpts, istioNamespace, kubeClient, userWriter)
}

// nolint: lll
func multiRequestAndProcessXds(all bool, dr *xdsapi.DiscoveryRequest, centralOpts *clioptions.CentralControlPlaneOptions, istioNamespace string, kubeClient kube.ExtendedClient, userWriter io.Writer) (map[string]*xdsapi.DiscoveryResponse, error) {

	// If Central Istiod case, just call it
	if centralOpts.Xds != "" {
		response, err := xds.GetXdsResponse(dr, centralOpts, useToken(centralOpts, kubeClient, userWriter))
		if err != nil {
			return nil, err
		}
		return map[string]*xdsapi.DiscoveryResponse{
			CpInfo(response).ID: response,
		}, nil
	}

	// Self-administered case.  Find all Istiods in revision using K8s, port-forward and call each in turn
	responses, err := queryEachShard(all, dr, istioNamespace, kubeClient, centralOpts, userWriter)
	if err != nil {
		return nil, err
	}
	return mapShards(responses)
}

func mapShards(responses []*xdsapi.DiscoveryResponse) (map[string]*xdsapi.DiscoveryResponse, error) {
	retval := map[string]*xdsapi.DiscoveryResponse{}

	for _, response := range responses {
		retval[CpInfo(response).ID] = response
	}

	return retval, nil
}

// CpInfo returns the Istio control plane info from JSON-encoded XDS ControlPlane Identifier
func CpInfo(xdsResponse *xdsapi.DiscoveryResponse) pilotxds.IstioControlPlaneInstance {
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

// If a token might help (it won't if we have certs or plaintext), create a TokenReader
func useToken(opts *clioptions.CentralControlPlaneOptions, kubeClient kube.ExtendedClient, userWriter io.Writer) adsc.TokenReader {
	// If we are using the insecure 15010 don't bother getting a token
	if opts.Plaintext || opts.CertDir != "" {
		return nil
	}

	return istioctlTokenSupplier{
		opts:       opts,
		kubeClient: kubeClient,
		userWriter: userWriter,
	}
}

func (its istioctlTokenSupplier) Token() (string, error) {
	token, err := its.getCachedToken()
	if err == nil {
		err = its.reviewToken(context.TODO(), token)
	}
	if err != nil {
		token, err = its.createToken(context.TODO())
		if err != nil {
			return "", err
		}
	}
	err = its.saveToken(token)
	if err != nil {
		fmt.Fprintf(its.userWriter, "Could not cache token: %v\n", err)
	}
	return token, nil
}

func (its istioctlTokenSupplier) reviewToken(ctx context.Context, token string) error {
	tr, err := its.kubeClient.Kube().AuthenticationV1().TokenReviews().Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token:     token,
			Audiences: []string{"istio-ca"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		// This kind of error means we talked to the API Server.  It doesn't meet the token is valid.
		return err
	}
	if tr.Status.Error != "" {
		// This kind of error means something is wrong with the JWT itself
		fmt.Fprintf(its.userWriter, "Cached JWT token invalid; will refresh: %s\n", tr.Status.Error)
		return errors.New(tr.Status.Error)
	}
	return nil
}

func (its istioctlTokenSupplier) createToken(ctx context.Context) (string, error) {
	return createServiceAccountToken(ctx, its.kubeClient.Kube(), "default", "default")
}

func createServiceAccountToken(ctx context.Context, client kubernetes.Interface, ns string, serviceAccount string) (string, error) {
	expirationSeconds := int64(defaultExpirationSeconds)
	token, err := client.CoreV1().ServiceAccounts(ns).CreateToken(ctx, serviceAccount,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences:         []string{"istio-ca"},
				ExpirationSeconds: &expirationSeconds,
			},
		}, metav1.CreateOptions{})

	if err != nil {
		return "", err
	}
	return token.Status.Token, nil
}

func (its istioctlTokenSupplier) getCachedToken() (string, error) {
	file, err := os.Open(its.opts.JWTFile)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(its.userWriter, "failed to close %q: %s\n", its.opts.JWTFile, err)
		}
	}()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (its istioctlTokenSupplier) saveToken(token string) error {
	file, err := os.OpenFile(its.opts.JWTFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(its.userWriter, "failed to close %q: %s\n", its.opts.JWTFile, err)
		}
	}()
	_, err = file.WriteString(token)
	return err
}
