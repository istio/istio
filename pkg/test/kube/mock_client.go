//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"bytes"
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/pkg/version"

	"istio.io/istio/pkg/kube"
)

var _ kube.Client = MockClient{}

// MockClient for tests that rely on kube.Client.
type MockClient struct {
	*kubernetes.Clientset
	*rest.RESTClient
	// Results is a map of podName to the results of the expected test on the pod
	Results          map[string][]byte
	DiscoverablePods map[string]map[string]*v1.PodList
	RevisionValue    string
	ConfigValue      *rest.Config
	IstioVersions    *version.MeshInfo
}

func (c MockClient) AllDiscoveryDo(_ context.Context, _, _ string) (map[string][]byte, error) {
	return c.Results, nil
}

func (c MockClient) EnvoyDo(_ context.Context, podName, _, _, _ string, _ []byte) ([]byte, error) {
	results, ok := c.Results[podName]
	if !ok {
		return nil, fmt.Errorf("unable to retrieve Pod: pods %q not found", podName)
	}
	return results, nil
}

func (c MockClient) Config() *rest.Config {
	return c.ConfigValue
}

func (c MockClient) GetIstioVersions(_ context.Context, _ string) (*version.MeshInfo, error) {
	return c.IstioVersions, nil
}

func (c MockClient) PodsForSelector(_ context.Context, namespace, labelSelector string) (*v1.PodList, error) {
	podsForNamespace, ok := c.DiscoverablePods[namespace]
	if !ok {
		return &v1.PodList{}, nil
	}
	podsForLabel, ok := podsForNamespace[labelSelector]
	if !ok {
		return &v1.PodList{}, nil
	}
	return podsForLabel, nil
}

func (c MockClient) Revision() string {
	return c.RevisionValue
}

func (c MockClient) Ext() clientset.Interface {
	panic("not implemented by mock")
}

func (c MockClient) GetKubernetesVersion() (*kubeVersion.Info, error) {
	return nil, fmt.Errorf("TODO MockClient doesn't implement kubernetes version")
}

func (c MockClient) GetIstioPods(_ context.Context, _ string, _ map[string]string) ([]v1.Pod, error) {
	return nil, fmt.Errorf("TODO MockClient doesn't implement IstioPods")
}

func (c MockClient) PodExec(_, _, _ string, _ []string) (*bytes.Buffer, *bytes.Buffer, error) {
	return nil, nil, fmt.Errorf("TODO MockClient doesn't implement exec")
}

func (c MockClient) PodLogs(_ context.Context, _ string, _ string, _ string, _ bool) (string, error) {
	return "", fmt.Errorf("TODO MockClient doesn't implement logs")
}

func (c MockClient) NewPortForwarder(_, _, _ string, _, _ int) (kube.PortForwarder, error) {
	return nil, fmt.Errorf("TODO MockClient doesn't implement port forwarding")
}
