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
	"net/http"

	"google.golang.org/grpc/credentials"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kubeExtInformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/metadatainformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/rest/fake"
	"k8s.io/client-go/tools/cache"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	"k8s.io/kubectl/pkg/cmd/util"
	serviceapisclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	serviceapisinformer "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"
	mcsapisclient "sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"
	mcsapisinformer "sigs.k8s.io/mcs-api/pkg/client/informers/externalversions"

	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformer "istio.io/client-go/pkg/informers/externalversions"
	"istio.io/pkg/version"
)

var _ ExtendedClient = MockClient{}

type MockPortForwarder struct{}

func (m MockPortForwarder) Start() error {
	return nil
}

func (m MockPortForwarder) Address() string {
	return "localhost:3456"
}

func (m MockPortForwarder) Close() {
}

func (m MockPortForwarder) WaitForStop() {
}

var _ PortForwarder = MockPortForwarder{}

// MockClient for tests that rely on kube.Client.
type MockClient struct {
	kubernetes.Interface
	RestClient *rest.RESTClient
	// Results is a map of podName to the results of the expected test on the pod
	Results           map[string][]byte
	DiscoverablePods  map[string]map[string]*v1.PodList
	RevisionValue     string
	ConfigValue       *rest.Config
	IstioVersions     *version.MeshInfo
	KubernetesVersion uint
	IstiodVersion     string
}

func (c MockClient) SetPortManager(manager PortManager) {
}

func (c MockClient) WaitForCacheSync(stop <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool {
	return WaitForCacheSync(stop, cacheSyncs...)
}

func (c MockClient) ExtInformer() kubeExtInformers.SharedInformerFactory {
	panic("not used in mock")
}

func (c MockClient) Istio() istioclient.Interface {
	panic("not used in mock")
}

func (c MockClient) GatewayAPI() serviceapisclient.Interface {
	panic("not used in mock")
}

func (c MockClient) MCSApis() mcsapisclient.Interface {
	panic("not used in mock")
}

func (c MockClient) IstioInformer() istioinformer.SharedInformerFactory {
	panic("not used in mock")
}

func (c MockClient) GatewayAPIInformer() serviceapisinformer.SharedInformerFactory {
	panic("not used in mock")
}

func (c MockClient) MCSApisInformer() mcsapisinformer.SharedInformerFactory {
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

func (c MockClient) EnvoyDo(ctx context.Context, podName, podNamespace, method, path string) ([]byte, error) {
	results, ok := c.Results[podName]
	if !ok {
		return nil, fmt.Errorf("unable to retrieve Pod: pods %q not found", podName)
	}
	return results, nil
}

func (c MockClient) EnvoyDoWithPort(ctx context.Context, podName, podNamespace, method, path string, port int) ([]byte, error) {
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
	if c.IstiodVersion != "" {
		server := version.BuildInfo{}
		setServerInfoWithIstiodVersionInfo(&server, c.IstiodVersion)
		return &version.MeshInfo{
			{
				Info: server,
			},
		}, nil
	}
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
	return nil
}

func (c MockClient) ApplyYAMLFilesDryRun(string, ...string) error {
	panic("not implemented by mock")
}

// CreatePerRPCCredentials -- when implemented -- mocks per-RPC credentials (bearer token)
func (c MockClient) CreatePerRPCCredentials(ctx context.Context, tokenNamespace, tokenServiceAccount string, audiences []string,
	expirationSeconds int64,
) (credentials.PerRPCCredentials, error) {
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
	minor := fmt.Sprint(c.KubernetesVersion)
	if c.KubernetesVersion == 0 {
		minor = "16"
	}
	return &kubeVersion.Info{
		Major: "1",
		Minor: minor,
	}, nil
}

func (c MockClient) GetIstioPods(_ context.Context, _ string, _ map[string]string) ([]v1.Pod, error) {
	return nil, fmt.Errorf("TODO MockClient doesn't implement IstioPods")
}

func (c MockClient) PodExecCommands(podName, podNamespace, container string, commands []string) (stdout string, stderr string, err error) {
	return "", "", fmt.Errorf("TODO MockClient doesn't implement exec")
}

func (c MockClient) PodExec(_, _, _ string, _ string) (string, string, error) {
	return "", "", fmt.Errorf("TODO MockClient doesn't implement exec")
}

func (c MockClient) PodLogs(_ context.Context, _ string, _ string, _ string, _ bool) (string, error) {
	return "", fmt.Errorf("TODO MockClient doesn't implement logs")
}

func (c MockClient) NewPortForwarder(_, _, _ string, _, _ int) (PortForwarder, error) {
	return MockPortForwarder{}, nil
}

// UtilFactory mock's kubectl's utility factory.  This code sets up a fake factory,
// similar to the one in https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/describe/describe_test.go
func (c MockClient) UtilFactory() util.Factory {
	tf := cmdtesting.NewTestFactory()
	_, _, codec := cmdtesting.NewExternalScheme()
	tf.UnstructuredClient = &fake.RESTClient{
		NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
		Resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     cmdtesting.DefaultHeader(),
			Body: cmdtesting.ObjBody(codec,
				cmdtesting.NewInternalType("", "", "foo")),
		},
	}
	return tf
}
