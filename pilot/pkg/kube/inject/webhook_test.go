// Copyright 2018 Istio Authors
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

package inject

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/onsi/gomega"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/proto/hapi/chart"
	tversion "k8s.io/helm/pkg/proto/hapi/version"
	"k8s.io/helm/pkg/timeconv"
	"k8s.io/kubernetes/pkg/apis/core"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/mcp/testing/testcerts"
)

const (
	helmChartDirectory     = "../../../../install/kubernetes/helm/istio"
	helmConfigMapKey       = "istio/templates/sidecar-injector-configmap.yaml"
	helmValuesFile         = "values.yaml"
	yamlSeparator          = "\n---"
	minimalSidecarTemplate = `
initContainers:
- name: istio-init
containers:
- name: istio-proxy
volumes:
- name: istio-envoy
imagePullSecrets:
- name: istio-image-pull-secrets
`
)

var (
	rotatedKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA3Tr24CaBegyfkdDGWckqMHEWvpJBThjXlMz/FKcg1bgq57OD
oNHXN4dcyPCHWWEY3Eo3YG1es4pqTkvzK0+1JoY6/K88Lu1ePj5PeSFuWfPWi1BW
9oyWJW+AAzqqGkZmSo4z26N+E7N8ht5bTBMNVD3jqz9+MaqCTVmQ6dAgdFKH07wd
XWh6kKoh2g9bgBKB+qWrezmVRb31i93sM1pJos35cUmIbWgiSQYuSXEInitejcGZ
cBjqRy61SiZB7nbmMoew0G0aGXe20Wx+QiyJjbt9XNUm0IvjAJ1SSiPqfFQ4F+tx
K4q3xAwp1smyiMv57RNC2ny8YMntZYgQDDkhBQIDAQABAoIBAQDZHK396yw0WEEd
vFN8+CRUaBfXLPe0KkMgAFLxtNdPhy9sNsueP3HESC7x8MQUHmtkfd1837kJ4HRV
pMnfnpj8Vs17AIrCzycnVMVv7jQ7SUcrb8v4qJ4N3TA3exJHOQHYd1hDXF81/Hbg
cUYOEcCKBTby8BvrqBe6y4ShQiUnoaeeM5j9x32+QB652/9PMuZJ9xfwyoEBjoVA
cccp+u3oBX864ztaG9Gn0wbgRVeafsPfuAOUmShykohV1mVJddiA0wayxGi0TmoK
dwrltdToI7BmpmmTLc59O44JFGwO67aJQHsrHBjEnpWlxFDwbfZuf93FgdFUFFjr
tVx2dPF9AoGBAPkIaUYxMSW78MD9862eJlGS6F/SBfPLTve3l9M+UFnEsWapNz+V
anlupaBtlfRJxLDzjheMVZTv/TaFIrrMdN/xc/NoYj6lw5RV8PEfKPB0FjSAqSEl
iVOA5q4kuI1xEeV7xLE4uJUF3wdoHz9cSmjrXDVZXq/KsaInABlAl1xjAoGBAONr
bgFUlQ+fbsGNjeefrIXBSU93cDo5tkd4+lefrmUVTwCo5V/AvMMQi88gV/sz6qCJ
gR0hXOqdQyn++cuqfMTDj4lediDGFEPho+n4ToIvkA020NQ05cKxcmn/6Ei+P9pk
v+zoT9RiVnkBje2n/KU2d/PEL9Nl4gvvAgPLt8V3AoGAZ6JZdQ15n3Nj0FyecKz0
01Oogl+7fGYqGap8cztmYsUY8lkPFdXPNnOWV3njQoMEaIMiqagL4Wwx2uNyvXvi
U2N+1lelMt720h8loqJN/irBJt44BARD7s0gsm2zo6DfSrnD8+Bf6BxGYSWyg0Kb
8KepesYTQmK+o3VJdDjOBHMCgYAIxbwYkQqu75d2H9+5b49YGXyadCEAHfnKCACg
IKi5fXjurZUrfGPLonfCJZ0/M2F5j9RLK15KLobIt+0qzgjCDkkbI2mrGfjuJWYN
QGbG3s7Ps62age/a8r1XGWf8ZlpQMlK08MEjkCeFw2mWIUS9mrxFyuuNXAC8NRv+
yXztQQKBgQDWTFFQdeYfuiKHrNmgOmLVuk1WhAaDgsdK8RrnNZgJX9bd7n7bm7No
GheN946AYsFge4DX7o0UXXJ3h5hTFn/hSWASI54cO6WyWNEiaP5HRlZqK7Jfej7L
mz+dlU3j/BY19RLmYeg4jFV4W66CnkDqpneOJs5WdmFFoWnHn7gRBw==
-----END RSA PRIVATE KEY-----`)

	// ServerCert is a test cert for dynamic admission controller.
	rotatedCert = []byte(`-----BEGIN CERTIFICATE-----
MIIDATCCAemgAwIBAgIJAJwGb32Zn8sDMA0GCSqGSIb3DQEBCwUAMA4xDDAKBgNV
BAMMA19jYTAgFw0xODAzMTYxNzI0NDJaGA8yMjkxMTIzMDE3MjQ0MlowEjEQMA4G
A1UEAwwHX3NlcnZlcjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAN06
9uAmgXoMn5HQxlnJKjBxFr6SQU4Y15TM/xSnINW4Kuezg6DR1zeHXMjwh1lhGNxK
N2BtXrOKak5L8ytPtSaGOvyvPC7tXj4+T3khblnz1otQVvaMliVvgAM6qhpGZkqO
M9ujfhOzfIbeW0wTDVQ946s/fjGqgk1ZkOnQIHRSh9O8HV1oepCqIdoPW4ASgfql
q3s5lUW99Yvd7DNaSaLN+XFJiG1oIkkGLklxCJ4rXo3BmXAY6kcutUomQe525jKH
sNBtGhl3ttFsfkIsiY27fVzVJtCL4wCdUkoj6nxUOBfrcSuKt8QMKdbJsojL+e0T
Qtp8vGDJ7WWIEAw5IQUCAwEAAaNcMFowCQYDVR0TBAIwADALBgNVHQ8EBAMCBeAw
HQYDVR0lBBYwFAYIKwYBBQUHAwIGCCsGAQUFBwMBMCEGA1UdEQQaMBiHBH8AAAGH
EAAAAAAAAAAAAAAAAAAAAAEwDQYJKoZIhvcNAQELBQADggEBACbBlWo/pY/OIJaW
RwkfSRVzEIWpHt5OF6p93xfyy4/zVwwhH1AQB7Euji8vOaVNOpMfGYLNH3KIRReC
CIvGEH4yZDbpiH2cOshqMCuV1CMRUTdl4mq6M0PtGm6b8OG3uIFTLIR973LBWOl5
wCR1yrefT1NHuIScGaBXUGAV4JAx37pfg84hDD73T2j1TDD3Lrmsb9WCP+L26TG6
ICN61cIhgz8wChQpF8/fFAI5Fjbjrz5C1Xw/EUHLf/TTn/7Yfp2BHsGm126Et+k+
+MLBzBfrHKwPaGqDvNHUDrI6c3GI0Qp7jW93FbL5ul8JQ+AowoMF2dIEbN9qQEVP
ZOQ5UvU=
-----END CERTIFICATE-----`)
)

func parseToLabelSelector(t *testing.T, selector string) *metav1.LabelSelector {
	result, err := metav1.ParseToLabelSelector(selector)
	if err != nil {
		t.Errorf("Invalid selector %v: %v", selector, err)
	}

	return result
}

func TestInjectRequired(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	podSpecHostNetwork := &corev1.PodSpec{
		HostNetwork: true,
	}
	cases := []struct {
		config  *Config
		podSpec *corev1.PodSpec
		meta    *metav1.ObjectMeta
		want    bool
	}{
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "no-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "default-policy",
				Namespace: "test-namespace",
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-on-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotationPolicy.name: "true"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotationPolicy.name: "false"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "no-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "default-policy",
				Namespace: "test-namespace",
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-on-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotationPolicy.name: "true"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy: InjectionPolicyDisabled,
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotationPolicy.name: "false"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: InjectionPolicyEnabled,
			},
			podSpec: podSpecHostNetwork,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			config: &Config{
				Policy: "wrong_policy",
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "wrong-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyEnabled,
				AlwaysInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-always-inject-no-labels",
				Namespace: "test-namespace",
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyEnabled,
				AlwaysInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-always-inject-with-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "bar1"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-inject-no-labels",
				Namespace: "test-namespace",
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-inject-with-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "bar"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-never-inject-no-labels",
				Namespace: "test-namespace",
			},
			want: true,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-never-inject-with-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "bar"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyDisabled,
				NeverInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-never-inject-no-labels",
				Namespace: "test-namespace",
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyDisabled,
				NeverInjectSelector: []metav1.LabelSelector{{MatchLabels: map[string]string{"foo": "bar"}}},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-never-inject-with-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "bar"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-never-inject-with-empty-label",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-inject-with-empty-label",
				Namespace: "test-namespace",
				Labels:    map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "always")},
				NeverInjectSelector:  []metav1.LabelSelector{*parseToLabelSelector(t, "never")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-never-inject-with-label-returns-true",
				Namespace: "test-namespace",
				Labels:    map[string]string{"always": "bar", "foo2": "bar2"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "always")},
				NeverInjectSelector:  []metav1.LabelSelector{*parseToLabelSelector(t, "never")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-never-inject-with-label-returns-false",
				Namespace: "test-namespace",
				Labels:    map[string]string{"never": "bar", "foo2": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "always")},
				NeverInjectSelector:  []metav1.LabelSelector{*parseToLabelSelector(t, "never")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-disabled-always-never-inject-with-both-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"always": "bar", "never": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "policy-enabled-annotation-true-never-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotationPolicy.name: "true"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyEnabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "policy-enabled-annotation-false-always-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotationPolicy.name: "false"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "policy-disabled-annotation-false-always-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotationPolicy.name: "false"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyEnabled,
				NeverInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo"), *parseToLabelSelector(t, "bar")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-never-inject-multiple-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"label1": "", "bar": "anything"},
			},
			want: false,
		},
		{
			config: &Config{
				Policy:               InjectionPolicyDisabled,
				AlwaysInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo"), *parseToLabelSelector(t, "bar")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:      "policy-enabled-always-inject-multiple-labels",
				Namespace: "test-namespace",
				Labels:    map[string]string{"label1": "", "bar": "anything"},
			},
			want: true,
		},
		{
			config: &Config{
				Policy:              InjectionPolicyDisabled,
				NeverInjectSelector: []metav1.LabelSelector{*parseToLabelSelector(t, "foo")},
			},
			podSpec: podSpec,
			meta: &metav1.ObjectMeta{
				Name:        "policy-disabled-annotation-true-never-inject",
				Namespace:   "test-namespace",
				Annotations: map[string]string{annotationPolicy.name: "true"},
				Labels:      map[string]string{"foo": "", "foo2": "bar2"},
			},
			want: true,
		},
	}

	for _, c := range cases {
		if got := injectRequired(ignoredNamespaces, c.config, c.podSpec, c.meta); got != c.want {
			t.Errorf("injectRequired(%v, %v) got %v want %v", c.config, c.meta, got, c.want)
		}
	}
}

func TestInject(t *testing.T) {
	cases := []struct {
		inputFile string
		wantFile  string
	}{
		{
			inputFile: "TestWebhookInject.yaml",
			wantFile:  "TestWebhookInject.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers.yaml",
			wantFile:  "TestWebhookInject_no_initContainers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers.yaml",
			wantFile:  "TestWebhookInject_no_containers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_volumes.yaml",
			wantFile:  "TestWebhookInject_no_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_containers_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_containers_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_containers_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_containers.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_containers_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_containers_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers_volumes.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_containers_volumes.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_initContainers_containers_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_no_initContainers_containers_volumes_imagePullSecrets.yaml",
			wantFile:  "TestWebhookInject_no_initcontainers_containers_volumes_imagePullSecrets.patch",
		},
		{
			inputFile: "TestWebhookInject_replace.yaml",
			wantFile:  "TestWebhookInject_replace.patch",
		},
		{
			inputFile: "TestWebhookInject_replace_backwards_compat.yaml",
			wantFile:  "TestWebhookInject_replace_backwards_compat.patch",
		},
	}

	for i, c := range cases {
		input := filepath.Join("testdata/webhook", c.inputFile)
		want := filepath.Join("testdata/webhook", c.wantFile)
		t.Run(fmt.Sprintf("[%d] %s", i, c.inputFile), func(t *testing.T) {
			wh := createTestWebhookFromFile("testdata/webhook/TestWebhookInject_template.yaml", t)
			podYAML := util.ReadFile(input, t)
			podJSON, err := yaml.YAMLToJSON(podYAML)
			if err != nil {
				t.Fatalf(err.Error())
			}
			got := wh.inject(&v1beta1.AdmissionReview{
				Request: &v1beta1.AdmissionRequest{
					Object: runtime.RawExtension{
						Raw: podJSON,
					},
				},
			})
			var prettyPatch bytes.Buffer
			if err := json.Indent(&prettyPatch, got.Patch, "", "  "); err != nil {
				t.Fatalf(err.Error())
			}
			util.CompareContent(prettyPatch.Bytes(), want, t)
		})
	}
}

// TestHelmInject tests the webhook injector with the installation configmap.yaml. It runs through many of the
// same tests as TestIntoResourceFile in order to verify that the webhook performs the same way as the manual injector.
func TestHelmInject(t *testing.T) {
	// Create the webhook from the install configmap.
	webhook := createTestWebhookFromHelmConfigMap(t)

	// NOTE: this list is a subset of the list in TestIntoResourceFile. It contains all test cases that operate
	// on Deployments and do not require updates to the injection Params.
	cases := []struct {
		inputFile string
		wantFile  string
	}{
		{
			inputFile: "hello-probes.yaml",
			wantFile:  "hello-probes.yaml.injected",
		},
		{
			inputFile: "hello.yaml",
			wantFile:  "hello-config-map-name.yaml.injected",
		},
		{
			inputFile: "frontend.yaml",
			wantFile:  "frontend.yaml.injected",
		},
		{
			inputFile: "hello-multi.yaml",
			wantFile:  "hello-multi.yaml.injected",
		},
		{
			inputFile: "multi-init.yaml",
			wantFile:  "multi-init.yaml.injected",
		},
		{
			inputFile: "statefulset.yaml",
			wantFile:  "statefulset.yaml.injected",
		},
		{
			inputFile: "daemonset.yaml",
			wantFile:  "daemonset.yaml.injected",
		},
		{
			inputFile: "job.yaml",
			wantFile:  "job.yaml.injected",
		},
		{
			inputFile: "replicaset.yaml",
			wantFile:  "replicaset.yaml.injected",
		},
		{
			inputFile: "replicationcontroller.yaml",
			wantFile:  "replicationcontroller.yaml.injected",
		},
		{
			inputFile: "list.yaml",
			wantFile:  "list.yaml.injected",
		},
		{
			inputFile: "list-frontend.yaml",
			wantFile:  "list-frontend.yaml.injected",
		},
		{
			inputFile: "deploymentconfig.yaml",
			wantFile:  "deploymentconfig.yaml.injected",
		},
		{
			inputFile: "deploymentconfig-multi.yaml",
			wantFile:  "deploymentconfig-multi.yaml.injected",
		},
		{
			// Verifies that annotation values are applied properly. This also tests that annotation values
			// override params when specified.
			inputFile: "traffic-annotations.yaml",
			wantFile:  "traffic-annotations.yaml.injected",
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			inputFile: "traffic-annotations-wildcards.yaml",
			wantFile:  "traffic-annotations-wildcards.yaml.injected",
		},
		{
			// Verifies that the wildcard character "*" behaves properly when used in annotations.
			inputFile: "traffic-annotations-empty-includes.yaml",
			wantFile:  "traffic-annotations-empty-includes.yaml.injected",
		},
		{
			// Verifies that the status port annotation overrides the default.
			inputFile: "status_annotations.yaml",
			wantFile:  "status_annotations.yaml.injected",
		},
		{
			// Verifies that the resource annotation overrides the default.
			inputFile: "resource_annotations.yaml",
			wantFile:  "resource_annotations.yaml.injected",
		},
	}

	for ci, c := range cases {
		inputFile := filepath.Join("testdata/webhook", c.inputFile)
		wantFile := filepath.Join("testdata/webhook", c.wantFile)
		testName := fmt.Sprintf("[%02d] %s", ci, c.wantFile)
		t.Run(testName, func(t *testing.T) {
			// Split multi-part yaml documents. Input and output will have the same number of parts.
			inputYAMLs := splitYamlFile(inputFile, t)
			wantYAMLs := splitYamlFile(wantFile, t)
			goldenYAMLs := make([][]byte, len(inputYAMLs))

			for i := 0; i < len(inputYAMLs); i++ {
				t.Run(fmt.Sprintf("yamlPart[%d]", i), func(t *testing.T) {
					// Convert the input YAML to a deployment.
					inputYAML := inputYAMLs[i]
					inputJSON := yamlToJSON(inputYAML, t)
					inputDeployment := jsonToDeployment(inputJSON, t)

					// Convert the wanted YAML to a deployment.
					wantYAML := wantYAMLs[i]
					wantJSON := yamlToJSON(wantYAML, t)
					wantDeployment := jsonToDeployment(wantJSON, t)

					// Generate the patch.  At runtime, the webhook would actually generate the patch against the
					// pod configuration. But since our input files are deployments, rather than actual pod instances,
					// we have to apply the patch to the template portion of the deployment only.
					templateJSON := toJSON(inputDeployment.Spec.Template, t)
					got := webhook.inject(&v1beta1.AdmissionReview{
						Request: &v1beta1.AdmissionRequest{
							Object: runtime.RawExtension{
								Raw: templateJSON,
							},
						},
					})

					// Apply the generated patch to the template.
					patch := prettyJSON(got.Patch, t)
					patchedTemplateJSON := applyJSONPatch(templateJSON, patch, t)

					// Create the patched deployment. It's just a copy of the original, but with a patched template
					// applied.
					patchedDeployment := inputDeployment.DeepCopy()
					patchedDeployment.Spec.Template.Reset()
					if err := json.Unmarshal(patchedTemplateJSON, &patchedDeployment.Spec.Template); err != nil {
						t.Fatal(err)
					}

					if !util.Refresh() {
						// Compare the patched deployment with the one we expected.
						compareDeployments(patchedDeployment, wantDeployment, c.wantFile, t)
					} else {
						goldenYAMLs[i] = deploymentToYaml(patchedDeployment, t)
					}
				})
			}
			if util.Refresh() {
				writeYamlsToGoldenFile(goldenYAMLs, wantFile, t)
			}
		})
	}
}

func createTestWebhook(sidecarTemplate string) *Webhook {
	mesh := model.DefaultMeshConfig()
	return &Webhook{
		sidecarConfig: &Config{
			Policy:   InjectionPolicyEnabled,
			Template: sidecarTemplate,
		},
		sidecarTemplateVersion: "unit-test-fake-version",
		meshConfig:             &mesh,
	}
}

func createTestWebhookFromFile(templateFile string, t *testing.T) *Webhook {
	t.Helper()
	sidecarTemplate := string(util.ReadFile(templateFile, t))
	return createTestWebhook(sidecarTemplate)
}

func createTestWebhookFromHelmConfigMap(t *testing.T) *Webhook {
	t.Helper()
	// Load the config map with Helm. This simulates what will be done at runtime, by replacing function calls and
	// variables and generating a new configmap for use by the injection logic.
	sidecarTemplate := string(loadConfigMapWithHelm(t))
	return createTestWebhook(sidecarTemplate)
}

type configMapBody struct {
	Policy   string `yaml:"policy"`
	Template string `yaml:"template"`
}

func loadConfigMapWithHelm(t *testing.T) string {
	t.Helper()
	c, err := chartutil.Load(helmChartDirectory)
	if err != nil {
		t.Fatal(err)
	}

	values := getHelmValues(t)
	config := &chart.Config{Raw: values, Values: map[string]*chart.Value{}}
	options := chartutil.ReleaseOptions{
		Name:      "istio",
		Time:      timeconv.Now(),
		Namespace: "",
	}

	vals, err := chartutil.ToRenderValuesCaps(c, config, options, &chartutil.Capabilities{TillerVersion: &tversion.Version{SemVer: "2.7.2"}})
	if err != nil {
		t.Fatal(err)
	}

	files, err := engine.New().Render(c, vals)
	if err != nil {
		t.Fatal(err)
	}

	f, ok := files[helmConfigMapKey]
	if !ok {
		t.Fatalf("Unable to located configmap file %s", helmConfigMapKey)
	}

	cfgMap := core.ConfigMap{}
	err = yaml.Unmarshal([]byte(f), &cfgMap)
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := cfgMap.Data["config"]
	if !ok {
		t.Fatal("ConfigMap yaml missing config field")
	}

	body := &configMapBody{}
	err = yaml.Unmarshal([]byte(cfg), body)
	if err != nil {
		t.Fatal(err)
	}

	return body.Template
}

func getHelmValues(t *testing.T) string {
	t.Helper()
	valuesFile := filepath.Join(helmChartDirectory, helmValuesFile)
	return string(util.ReadFile(valuesFile, t))
}

func splitYamlFile(yamlFile string, t *testing.T) [][]byte {
	t.Helper()
	yamlBytes := util.ReadFile(yamlFile, t)
	return splitYamlBytes(yamlBytes, t)
}

func splitYamlBytes(yaml []byte, t *testing.T) [][]byte {
	t.Helper()
	stringParts := strings.Split(string(yaml), yamlSeparator)
	byteParts := make([][]byte, 0)
	for _, stringPart := range stringParts {
		byteParts = append(byteParts, getInjectableYamlDocs(stringPart, t)...)
	}
	if len(byteParts) == 0 {
		t.Skip("Found no injectable parts")
	}
	return byteParts
}

func writeYamlsToGoldenFile(yamls [][]byte, goldenFile string, t *testing.T) {
	content := make([]byte, 0)
	for _, part := range(yamls) {
		content = append(content, part...)
		content = append(content, []byte(yamlSeparator)...)
		content = append(content, '\n')
	}

	util.RefreshGoldenFile(content, goldenFile, t)
}
func getInjectableYamlDocs(yamlDoc string, t *testing.T) [][]byte {
	t.Helper()
	m := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(yamlDoc), &m); err != nil {
		t.Fatal(err)
	}
	switch m["kind"] {
	case "Deployment":
		return [][]byte{[]byte(yamlDoc)}
	case "DeploymentConfig":
		return [][]byte{[]byte(yamlDoc)}
	case "DaemonSet":
		return [][]byte{[]byte(yamlDoc)}
	case "StatefulSet":
		return [][]byte{[]byte(yamlDoc)}
	case "Job":
		return [][]byte{[]byte(yamlDoc)}
	case "ReplicaSet":
		return [][]byte{[]byte(yamlDoc)}
	case "ReplicationController":
		return [][]byte{[]byte(yamlDoc)}
	case "CronJob":
		return [][]byte{[]byte(yamlDoc)}
	case "List":
		// Split apart the list into separate yaml documents.
		out := make([][]byte, 0)
		list := metav1.List{}
		if err := yaml.Unmarshal([]byte(yamlDoc), &list); err != nil {
			t.Fatal(err)
		}
		for _, i := range list.Items {
			iout, err := yaml.Marshal(i)
			if err != nil {
				t.Fatal(err)
			}
			injectables := getInjectableYamlDocs(string(iout), t)
			out = append(out, injectables...)
		}
		return out
	default:
		// No injectable parts.
		return [][]byte{}
	}
}

func toJSON(i interface{}, t *testing.T) []byte {
	t.Helper()
	outputJSON, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	return prettyJSON(outputJSON, t)
}

func yamlToJSON(inputYAML []byte, t *testing.T) []byte {
	t.Helper()
	// Convert to JSON
	out, err := yaml.YAMLToJSON(inputYAML)
	if err != nil {
		t.Fatalf(err.Error())
	}
	return prettyJSON(out, t)
}

func prettyJSON(inputJSON []byte, t *testing.T) []byte {
	t.Helper()
	// Pretty-print the JSON
	var prettyBuffer bytes.Buffer
	if err := json.Indent(&prettyBuffer, inputJSON, "", "  "); err != nil {
		t.Fatalf(err.Error())
	}
	return prettyBuffer.Bytes()
}

func applyJSONPatch(input, patch []byte, t *testing.T) []byte {
	t.Helper()
	p, err := jsonpatch.DecodePatch(patch)
	if err != nil {
		t.Fatal(err)
	}

	patchedJSON, err := p.Apply(input)
	if err != nil {
		t.Fatal(err)
	}
	return prettyJSON(patchedJSON, t)
}

func jsonToDeployment(deploymentJSON []byte, t *testing.T) *extv1beta1.Deployment {
	t.Helper()
	var deployment extv1beta1.Deployment
	if err := json.Unmarshal(deploymentJSON, &deployment); err != nil {
		t.Fatal(err)
	}
	return &deployment
}

func deploymentToYaml(deployment *extv1beta1.Deployment, t *testing.T) []byte {
	t.Helper()
	yaml, err := yaml.Marshal(deployment)
	if err != nil {
		t.Fatal(err)
	}
	return yaml
}

func compareDeployments(got, want *extv1beta1.Deployment, name string, t *testing.T) {
	t.Helper()
	// Scrub unimportant fields that tend to differ.
	annotations(got)[annotationStatus.name] = annotations(want)[annotationStatus.name]
	gotIstioCerts := istioCerts(got)
	wantIstioCerts := istioCerts(want)
	gotIstioCerts.Secret.DefaultMode = wantIstioCerts.Secret.DefaultMode
	gotIstioInit := istioInit(got, t)
	wantIstioInit := istioInit(want, t)
	gotIstioInit.Image = wantIstioInit.Image
	gotIstioInit.TerminationMessagePath = wantIstioInit.TerminationMessagePath
	gotIstioInit.TerminationMessagePolicy = wantIstioInit.TerminationMessagePolicy
	gotIstioInit.SecurityContext.Privileged = wantIstioInit.SecurityContext.Privileged
	gotIstioProxy := istioProxy(got, t)

	wantIstioProxy := istioProxy(want, t)

	gotIstioProxy.Image = wantIstioProxy.Image
	gotIstioProxy.TerminationMessagePath = wantIstioProxy.TerminationMessagePath
	gotIstioProxy.TerminationMessagePolicy = wantIstioProxy.TerminationMessagePolicy

	// collect automatically injected pod labels so that they can
	// be adjusted later.
	envNames := map[string]bool{}
	for k := range got.Spec.Template.ObjectMeta.Labels {
		envNames["ISTIO_META_"+k] = true
	}

	envVars := make([]corev1.EnvVar, 0)
	for _, env := range gotIstioProxy.Env {
		if env.ValueFrom != nil {
			env.ValueFrom.FieldRef.APIVersion = ""
		}
		// adjust for injected var names.
		if _, found := envNames[env.Name]; found || strings.HasPrefix(env.Name, "ISTIO_METAB64_") {
			continue
		}
		envVars = append(envVars, env)
	}
	gotIstioProxy.Env = envVars

	marshaler := jsonpb.Marshaler{
		Indent: "  ",
	}
	gotString, err := marshaler.MarshalToString(got)
	if err != nil {
		t.Fatal(err)
	}
	wantString, err := marshaler.MarshalToString(want)
	if err != nil {
		t.Fatal(err)
	}
	util.CompareBytes([]byte(gotString), []byte(wantString), name, t)
}

func annotations(d *extv1beta1.Deployment) map[string]string {
	return d.Spec.Template.ObjectMeta.Annotations
}

func istioCerts(d *extv1beta1.Deployment) *corev1.Volume {
	for i := 0; i < len(d.Spec.Template.Spec.Volumes); i++ {
		v := &d.Spec.Template.Spec.Volumes[i]
		if v.Name == "istio-certs" {
			return v
		}
	}
	return nil
}

func istioInit(d *extv1beta1.Deployment, t *testing.T) *corev1.Container {
	t.Helper()
	for i := 0; i < len(d.Spec.Template.Spec.InitContainers); i++ {
		c := &d.Spec.Template.Spec.InitContainers[i]
		if c.Name == "istio-init" {
			return c
		}
	}
	t.Fatal("Failed to find istio-init container")
	return nil
}

func istioProxy(d *extv1beta1.Deployment, t *testing.T) *corev1.Container {
	t.Helper()
	for i := 0; i < len(d.Spec.Template.Spec.Containers); i++ {
		c := &d.Spec.Template.Spec.Containers[i]
		if c.Name == "istio-proxy" {
			return c
		}
	}
	t.Fatal("Failed to find istio-proxy container")
	return nil
}

func makeTestData(t testing.TB, skip bool) []byte {
	t.Helper()

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			Volumes:          []corev1.Volume{{Name: "v0"}},
			InitContainers:   []corev1.Container{{Name: "c0"}},
			Containers:       []corev1.Container{{Name: "c1"}},
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "p0"}},
		},
	}

	if skip {
		pod.ObjectMeta.Annotations[annotationPolicy.name] = "false"
	}

	raw, err := json.Marshal(&pod)
	if err != nil {
		t.Fatalf("Could not create test pod: %v", err)
	}

	review := v1beta1.AdmissionReview{
		Request: &v1beta1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{},
			Object: runtime.RawExtension{
				Raw: raw,
			},
			Operation: v1beta1.Create,
		},
	}
	reviewJSON, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("Failed to create AdmissionReview: %v", err)
	}
	return reviewJSON
}

func createWebhook(t testing.TB, sidecarTemplate string) (*Webhook, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "webhook_test")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	cleanup := func() {
		os.RemoveAll(dir) // nolint: errcheck
	}

	config := &Config{
		Policy:   InjectionPolicyEnabled,
		Template: sidecarTemplate,
	}
	configBytes, err := yaml.Marshal(config)
	if err != nil {
		cleanup()
		t.Fatalf("Could not marshal test injection config: %v", err)
	}

	var (
		configFile = filepath.Join(dir, "config-file.yaml")
		meshFile   = filepath.Join(dir, "mesh-file.yaml")
		certFile   = filepath.Join(dir, "cert-file.yaml")
		keyFile    = filepath.Join(dir, "key-file.yaml")
		port       = 0
	)

	if err := ioutil.WriteFile(configFile, configBytes, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", configFile, err)
	}

	// mesh
	mesh := model.DefaultMeshConfig()
	m := jsonpb.Marshaler{
		Indent: "  ",
	}
	var meshBytes bytes.Buffer
	if err := m.Marshal(&meshBytes, &mesh); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("yaml.Marshal(mesh) failed: %v", err)
	}
	if err := ioutil.WriteFile(meshFile, meshBytes.Bytes(), 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", meshFile, err)
	}

	// cert
	if err := ioutil.WriteFile(certFile, testcerts.ServerCert, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", certFile, err)
	}
	// key
	if err := ioutil.WriteFile(keyFile, testcerts.ServerKey, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", keyFile, err)
	}

	wh, err := NewWebhook(WebhookParameters{
		ConfigFile: configFile, MeshFile: meshFile, CertFile: certFile, KeyFile: keyFile, Port: port})
	if err != nil {
		cleanup()
		t.Fatalf("NewWebhook() failed: %v", err)
	}
	return wh, cleanup
}

func TestRunAndServe(t *testing.T) {
	wh, cleanup := createWebhook(t, minimalSidecarTemplate)
	defer cleanup()
	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)

	validReview := makeTestData(t, false)
	skipReview := makeTestData(t, true)

	// nolint: lll
	validPatch := []byte(`[
   {
      "op":"add",
      "path":"/spec/initContainers/-",
      "value":{
         "name":"istio-init",
         "resources":{

         }
      }
   },
   {
      "op":"add",
      "path":"/spec/containers/-",
      "value":{
         "name":"istio-proxy",
         "resources":{

         }
      }
   },
   {
      "op":"add",
      "path":"/spec/volumes/-",
      "value":{
         "name":"istio-envoy"
      }
   },
   {
      "op":"add",
      "path":"/spec/imagePullSecrets/-",
      "value":{
         "name":"istio-image-pull-secrets"
      }
   },
   {
      "op":"add",
      "path":"/metadata/annotations",
      "value":{
		  "sidecar.istio.io/status":"{\"version\":\"461c380844de8df1d1e2a80a09b6d7b58b8313c4a7d6796530eb124740a1440f\",\"initContainers\":[\"istio-init\"],\"containers\":[\"istio-proxy\"],\"volumes\":[\"istio-envoy\"],\"imagePullSecrets\":[\"istio-image-pull-secrets\"]}"
      }
   }
]`)

	cases := []struct {
		name           string
		body           []byte
		contentType    string
		wantAllowed    bool
		wantStatusCode int
		wantPatch      []byte
	}{
		{
			name:           "valid",
			body:           validReview,
			contentType:    "application/json",
			wantAllowed:    true,
			wantStatusCode: http.StatusOK,
			wantPatch:      validPatch,
		},
		{
			name:           "skipped",
			body:           skipReview,
			contentType:    "application/json",
			wantAllowed:    true,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "wrong content-type",
			body:           validReview,
			contentType:    "application/yaml",
			wantAllowed:    false,
			wantStatusCode: http.StatusUnsupportedMediaType,
		},
		{
			name:           "bad content",
			body:           []byte{0, 1, 2, 3, 4, 5}, // random data
			contentType:    "application/json",
			wantAllowed:    false,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "missing body",
			contentType:    "application/json",
			wantAllowed:    false,
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://sidecar-injector/inject", bytes.NewReader(c.body))
			req.Header.Add("Content-Type", c.contentType)

			w := httptest.NewRecorder()
			wh.serveInject(w, req)
			res := w.Result()

			if res.StatusCode != c.wantStatusCode {
				t.Fatalf("wrong status code: \ngot %v \nwant %v", res.StatusCode, c.wantStatusCode)
			}

			if res.StatusCode != http.StatusOK {
				return
			}

			gotBody, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("could not read body: %v", err)
			}
			var gotReview v1beta1.AdmissionReview
			if err := json.Unmarshal(gotBody, &gotReview); err != nil {
				t.Fatalf("could not decode response body: %v", err)
			}
			if gotReview.Response.Allowed != c.wantAllowed {
				t.Fatalf("AdmissionReview.Response.Allowed is wrong : got %v want %v",
					gotReview.Response.Allowed, c.wantAllowed)
			}

			var gotPatch bytes.Buffer
			if len(gotReview.Response.Patch) > 0 {
				if err := json.Compact(&gotPatch, gotReview.Response.Patch); err != nil {
					t.Fatalf(err.Error())
				}
			}
			var wantPatch bytes.Buffer
			if len(c.wantPatch) > 0 {
				if err := json.Compact(&wantPatch, c.wantPatch); err != nil {
					t.Fatalf(err.Error())
				}
			}

			if !bytes.Equal(gotPatch.Bytes(), wantPatch.Bytes()) {
				t.Fatalf("got bad patch: \n got %v \n want %v", gotPatch, wantPatch)
			}
		})
	}
}

func TestReloadCert(t *testing.T) {
	wh, cleanup := createWebhook(t, minimalSidecarTemplate)
	defer cleanup()
	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)
	checkCert(t, wh, testcerts.ServerCert, testcerts.ServerKey)
	// Update cert/key files.
	if err := ioutil.WriteFile(wh.certFile, rotatedCert, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", wh.certFile, err)
	}
	if err := ioutil.WriteFile(wh.keyFile, rotatedKey, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", wh.keyFile, err)
	}
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() bool {
		return checkCert(t, wh, rotatedCert, rotatedKey)
	}, "10s", "100ms").Should(gomega.BeTrue())
}

func checkCert(t *testing.T, wh *Webhook, cert, key []byte) bool {
	t.Helper()
	actual, err := wh.getCert(nil)
	if err != nil {
		t.Fatalf("fail to get certificate from webhook: %s", err)
	}
	expected, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("fail to load test certs.")
	}
	return bytes.Equal(actual.Certificate[0], expected.Certificate[0])
}

func BenchmarkInjectServe(b *testing.B) {
	mesh := model.DefaultMeshConfig()
	params := &Params{
		InitImage:           InitImageName(unitTestHub, unitTestTag, false),
		ProxyImage:          ProxyImageName(unitTestHub, unitTestTag, false),
		ImagePullPolicy:     "IfNotPresent",
		Verbosity:           DefaultVerbosity,
		SidecarProxyUID:     DefaultSidecarProxyUID,
		Version:             "12345678",
		Mesh:                &mesh,
		IncludeIPRanges:     DefaultIncludeIPRanges,
		IncludeInboundPorts: DefaultIncludeInboundPorts,
	}
	sidecarTemplate, err := GenerateTemplateFromParams(params)
	if err != nil {
		b.Fatalf("GenerateTemplateFromParams(%v) failed: %v", params, err)
	}
	wh, cleanup := createWebhook(b, sidecarTemplate)
	defer cleanup()

	stop := make(chan struct{})
	defer func() { close(stop) }()
	go wh.Run(stop)

	body := makeTestData(b, false)
	req := httptest.NewRequest("POST", "http://sidecar-injector/inject", bytes.NewReader(body))
	req.Header.Add("Content-Type", "application/json")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wh.serveInject(httptest.NewRecorder(), req)
	}
}
