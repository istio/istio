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
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/metadatainformer"
	"k8s.io/client-go/rest"
	serviceapisclient "sigs.k8s.io/service-apis/pkg/client/clientset/versioned"
	serviceapisinformer "sigs.k8s.io/service-apis/pkg/client/informers/externalversions"

	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformer "istio.io/client-go/pkg/informers/externalversions"

	"istio.io/pkg/version"

	"istio.io/istio/pkg/kube"
)

var _ kube.ExtendedClient = MockClient{}

// MockClient for tests that rely on kube.Client.
type MockClient struct {
	kubernetes.Interface
	RestClient *rest.RESTClient
	// Results is a map of podName to the results of the expected test on the pod
	Results          map[string][]byte
	DiscoverablePods map[string]map[string]*v1.PodList
	RevisionValue    string
	ConfigValue      *rest.Config
	IstioVersions    *version.MeshInfo
}

func (c MockClient) Istio() istioclient.Interface {
	panic("not used in mock")
}

func (c MockClient) ServiceApis() serviceapisclient.Interface {
	panic("not used in mock")
}

func (c MockClient) IstioInformer() istioinformer.SharedInformerFactory {
	panic("not used in mock")
}

func (c MockClient) ServiceApisInformer() serviceapisinformer.SharedInformerFactory {
	panic("not used in mock")
}

func (c MockClient) Metadata() metadata.Interface {
	panic("not used in mock")
}

func (c MockClient) KubeInformer() informers.SharedInformerFactory {
	panic("not used in mock")
}

func (c MockClient) DynamicInformer() dynamicinformer.DynamicSharedInformerFactory {
	panic("not used in mock")
}

func (c MockClient) MetadataInformer() metadatainformer.SharedInformerFactory {
	panic("not used in mock")
}

func (c MockClient) RunAndWait(stop <-chan struct{}) {
	panic("not used in mock")
}

func (c MockClient) Kube() kubernetes.Interface {
	return c.Interface
}

func (c MockClient) DynamicClient() dynamic.Interface {
	panic("not used in mock")
}

func (c MockClient) MetadataClient() metadata.Interface {
	panic("not used in mock")
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

func (c MockClient) RESTConfig() *rest.Config {
	return c.ConfigValue
}

func (c MockClient) GetIstioVersions(_ context.Context, _ string) (*version.MeshInfo, error) {
	return c.IstioVersions, nil
}

func (c MockClient) PodsForSelector(_ context.Context, namespace string, labelSelectors ...string) (*v1.PodList, error) {
	podsForNamespace, ok := c.DiscoverablePods[namespace]
	if !ok {
		return &v1.PodList{}, nil
	}
	var allPods v1.PodList
	for _, selector := range labelSelectors {
		pods, ok := podsForNamespace[selector]
		if !ok {
			return &v1.PodList{}, nil
		}
		allPods.TypeMeta = pods.TypeMeta
		allPods.ListMeta = pods.ListMeta
		allPods.Items = append(allPods.Items, pods.Items...)
	}
	return &allPods, nil
}

func (c MockClient) Revision() string {
	return c.RevisionValue
}

func (c MockClient) REST() rest.Interface {
	panic("not implemented by mock")
}

func (c MockClient) ApplyYAMLFiles(string, ...string) error {
	panic("not implemented by mock")
}

func (c MockClient) ApplyYAMLFilesDryRun(string, ...string) error {
	panic("not implemented by mock")
}

func (c MockClient) DeleteYAMLFiles(string, ...string) error {
	panic("not implemented by mock")
}

func (c MockClient) DeleteYAMLFilesDryRun(string, ...string) error {
	panic("not implemented by mock")
}

func (c MockClient) Ext() clientset.Interface {
	panic("not implemented by mock")
}

func (c MockClient) Dynamic() dynamic.Interface {
	panic("not implemented by mock")
}

func (c MockClient) GetKubernetesVersion() (*kubeVersion.Info, error) {
	return nil, fmt.Errorf("TODO MockClient doesn't implement kubernetes version")
}

func (c MockClient) GetIstioPods(_ context.Context, _ string, _ map[string]string) ([]v1.Pod, error) {
	return nil, fmt.Errorf("TODO MockClient doesn't implement IstioPods")
}

func (c MockClient) PodExec(_, _, _ string, _ string) (string, string, error) {
	return "", "", fmt.Errorf("TODO MockClient doesn't implement exec")
}

func (c MockClient) PodLogs(_ context.Context, _ string, _ string, _ string, _ bool) (string, error) {
	return "", fmt.Errorf("TODO MockClient doesn't implement logs")
}

func (c MockClient) NewPortForwarder(_, _, _ string, _, _ int) (kube.PortForwarder, error) {
	return nil, fmt.Errorf("TODO MockClient doesn't implement port forwarding")
}
