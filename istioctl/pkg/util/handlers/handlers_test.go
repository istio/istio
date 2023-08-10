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

package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	"k8s.io/kubectl/pkg/scheme"

	"istio.io/istio/pkg/test/util/assert"
)

func TestInferPodInfo(t *testing.T) {
	tests := []struct {
		proxyName     string
		namespace     string
		wantPodName   string
		wantNamespace string
	}{
		{
			proxyName:     "istio-ingressgateway-8d9697654-qdzgh.istio-system",
			namespace:     "kube-system",
			wantPodName:   "istio-ingressgateway-8d9697654-qdzgh",
			wantNamespace: "istio-system",
		},
		{
			proxyName:     "istio-ingressgateway-8d9697654-qdzgh.istio-system",
			namespace:     "",
			wantPodName:   "istio-ingressgateway-8d9697654-qdzgh",
			wantNamespace: "istio-system",
		},
		{
			proxyName:     "istio-ingressgateway-8d9697654-qdzgh",
			namespace:     "kube-system",
			wantPodName:   "istio-ingressgateway-8d9697654-qdzgh",
			wantNamespace: "kube-system",
		},
		{
			proxyName:     "istio-security-post-install-1.2.2-bm9w2.istio-system",
			namespace:     "istio-system",
			wantPodName:   "istio-security-post-install-1.2.2-bm9w2",
			wantNamespace: "istio-system",
		},
		{
			proxyName:     "istio-security-post-install-1.2.2-bm9w2.istio-system",
			namespace:     "",
			wantPodName:   "istio-security-post-install-1.2.2-bm9w2",
			wantNamespace: "istio-system",
		},
		{
			proxyName:     "service/istiod",
			namespace:     "",
			wantPodName:   "service/istiod",
			wantNamespace: "",
		},
		{
			proxyName:     "service/istiod",
			namespace:     "namespace",
			wantPodName:   "service/istiod",
			wantNamespace: "namespace",
		},
		{
			proxyName:     "service/istiod.istio-system",
			namespace:     "namespace",
			wantPodName:   "service/istiod",
			wantNamespace: "istio-system",
		},
		{
			proxyName:     "gateway.gateway.networking.k8s.io/istiod",
			namespace:     "",
			wantPodName:   "gateway.gateway.networking.k8s.io/istiod",
			wantNamespace: "",
		},
		{
			proxyName:     "gateway.gateway.networking.k8s.io/istiod",
			namespace:     "namespace",
			wantPodName:   "gateway.gateway.networking.k8s.io/istiod",
			wantNamespace: "namespace",
		},
		{
			proxyName:     "gateway.gateway.networking.k8s.io/istiod.istio-system",
			namespace:     "namespace",
			wantPodName:   "gateway.gateway.networking.k8s.io/istiod",
			wantNamespace: "istio-system",
		},
	}
	for _, tt := range tests {
		t.Run(strings.Split(tt.proxyName, ".")[0], func(t *testing.T) {
			gotPodName, gotNamespace := InferPodInfo(tt.proxyName, tt.namespace)
			if gotPodName != tt.wantPodName || gotNamespace != tt.wantNamespace {
				t.Errorf("unexpected podName and namespace: wanted %v %v got %v %v", tt.wantPodName, tt.wantNamespace, gotPodName, gotNamespace)
			}
		})
	}
}

func TestInferPodInfoFromTypedResource(t *testing.T) {
	tests := []struct {
		name          string
		wantPodName   string
		wantNamespace string
	}{
		{
			name:          "foo-abc.istio-system",
			wantPodName:   "foo-abc",
			wantNamespace: "istio-system",
		},
		{
			name:          "foo-abc",
			wantPodName:   "foo-abc",
			wantNamespace: "test",
		},
		{
			name:          "Deployment/foo.istio-system",
			wantPodName:   "foo-abc",
			wantNamespace: "istio-system",
		},
		{
			name:          "Deployment/foo",
			wantPodName:   "foo-abc",
			wantNamespace: "test",
		},
	}
	factory := cmdtesting.NewTestFactory().WithNamespace("test")
	ns := scheme.Codecs.WithoutConversion()
	codec := scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)
	factory.Client = &fake.RESTClient{
		GroupVersion:         schema.GroupVersion{Group: "", Version: "v1"},
		NegotiatedSerializer: ns,
		Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			switch p, m := req.URL.Path, req.Method; {
			case p == "/namespaces/istio-system/deployments/foo" && m == "GET":
				body := cmdtesting.ObjBody(codec, attachDeploy("istio-system"))
				return &http.Response{StatusCode: http.StatusOK, Header: cmdtesting.DefaultHeader(), Body: body}, nil
			case p == "/namespaces/test/deployments/foo" && m == "GET":
				body := cmdtesting.ObjBody(codec, attachDeploy("test"))
				return &http.Response{StatusCode: http.StatusOK, Header: cmdtesting.DefaultHeader(), Body: body}, nil
			default:
				t.Errorf("%s: unexpected request: %s %#v\n%#v", p, req.Method, req.URL, req)
				return nil, fmt.Errorf("unexpected request")
			}
		}),
	}
	getFirstPodFunc = func(client corev1client.PodsGetter, namespace string, selector string, timeout time.Duration, sortBy func([]*corev1.Pod) sort.Interface) (
		*corev1.Pod, int, error,
	) {
		return attachPod(namespace), 1, nil
	}
	for _, tt := range tests {
		t.Run(strings.Split(tt.name, ".")[0], func(t *testing.T) {
			gotPodName, gotNamespace, err := InferPodInfoFromTypedResource(tt.name, "test", factory)
			assert.NoError(t, err)
			if gotPodName != tt.wantPodName || gotNamespace != tt.wantNamespace {
				t.Errorf("unexpected podName and namespace: wanted %v %v got %v %v", tt.wantPodName, tt.wantNamespace, gotPodName, gotNamespace)
			}
		})
	}
}

func attachPod(ns string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "foo-abc", Namespace: ns, ResourceVersion: "10"},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyAlways,
			DNSPolicy:     corev1.DNSClusterFirst,
			Containers: []corev1.Container{
				{
					Name: "bar",
				},
			},
			InitContainers: []corev1.Container{
				{
					Name: "initfoo",
				},
			},
			EphemeralContainers: []corev1.EphemeralContainer{
				{
					EphemeralContainerCommon: corev1.EphemeralContainerCommon{
						Name: "debugger",
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}

func attachDeploy(ns string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{},
	}
}
