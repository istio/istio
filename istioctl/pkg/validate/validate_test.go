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

package validate

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"regexp"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	mixervalidate "istio.io/istio/mixer/pkg/validate"
	"istio.io/istio/pkg/test/env"
)

const (
	validDeploymentList = `
apiVersion: v1
items:
- apiVersion: apps/v1
  kind: Deployment
  metadata:
    labels:
      app: hello
      version: v1
    name: hello-v1
  spec:
    replicas: 1
    template:
      metadata:
        labels:
          app: hello
          version: v1
      spec:
        containers:
        - name: hello
          image: istio/examples-hello
          imagePullPolicy: IfNotPresent
          ports:
          - containerPort: 9080
- apiVersion: apps/v1
  kind: Deployment
  metadata:
    labels:
      app: details
      version: v2
    name: details
  spec:
    replicas: 1
    template:
      metadata:
        labels:
          app: details
          version: v1
      spec:
        containers:
        - name: details
          image: istio/examples-bookinfo-details-v1:1.10.1
          imagePullPolicy: IfNotPresent
          ports:
          - containerPort: 9080
kind: List
metadata:
  resourceVersion: ""
  selfLink: ""`
	invalidSvcList = `
apiVersion: v1
items:
  -
    apiVersion: v1
    kind: Service
    metadata:
      name: details
    spec:
      ports:
        -
          name: details
          port: 9080
  -
    apiVersion: v1
    kind: Service
    metadata:
      name: hello
    spec:
      ports:
        -
          port: 80
          protocol: TCP
kind: List
metadata:
  resourceVersion: ""`
	udpService = `
kind: Service
metadata:
  name: hello
spec:
  ports:
    -
      protocol: udp`
	skippedService = `
kind: Service
metadata:
  name: hello
  namespace: istio-system
spec:
  ports:
    -
      name: http
      port: 9080`
	validPortNamingSvc = `
apiVersion: v1
kind: Service
metadata:
  name: hello
spec:
  ports:
    - name: http
      port: 9080`
	validPortNamingWithSuffixSvc = `
apiVersion: v1
kind: Service
metadata:
  name: hello
spec:
  ports:
    - name: http-hello
      port: 9080`
	invalidPortNamingSvc = `
apiVersion: v1
kind: Service
metadata:
  name: hello
spec:
  ports:
    - name: hello
      port: 9080`
	portNameMissingSvc = `
apiVersion: v1
kind: Service
metadata:
  name: hello
spec:
  ports:
  - protocol: TCP`
	validVirtualService = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: valid-virtual-service
spec:
  hosts:
    - c
  http:
    - route:
      - destination:
          host: c
          subset: v1
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 25`
	validVirtualService1 = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: valid-virtual-service1
spec:
  hosts:
    - d
  http:
    - route:
      - destination:
          host: c
          subset: v1
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 25`
	invalidVirtualService = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: invalid-virtual-service
spec:
  http:
    - route:
      - destination:
          host: c
          subset: v1
        weight: 75
      - destination:
          host: c
          subset: v2
        weight: 25`
	invalidVirtualServiceV1Beta1 = `
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: invalid-virtual-service
spec:
  http:
`
	validMixerRule = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: valid-rule
spec:
  match: request.headers["clnt"] == "abc"
  actions:
  - handler: handler-for-valid-rule.denier
    instances:
    - instance-for-valid-rule.checknothing`
	invalidMixerRule = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: valid-rule
spec:
  badField: oops
  match: request.headers["clnt"] == "abc"
  actions:
  - handler: handler-for-valid-rule.denier
    instances:
    - instance-for-valid-rule.checknothing`
	invalidYAML = `
(...!)`
	validKubernetesYAML = `
apiVersion: v1
kind: Namespace
metadata:
  name: istio-system`
	invalidMixerKind = `
apiVersion: config.istio.io/v1alpha2
kind: instance
metadata:
  name: invalid-kind
spec:`
	invalidUnsupportedKey = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage
unexpected_junk:
   still_more_junk:
spec:
  host: productpage`
	versionLabelMissingDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello
spec:`
	skippedDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello
  namespace: istio-system
spec: ~`
	invalidIstioConfig = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
  name: example-istiocontrolplane
spec:
  dummy:
  traffic_management:
    components:
    namespace: istio-traffic-management
`
	validIstioConfig = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
  name: example-istiocontrolplane
spec:
  addonComponents:
    grafana:
      enabled: true
`
	invalidDuplicateKey = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: productpage
spec:
trafficPolicy: {}
trafficPolicy:
  tls:
    mode: ISTIO_MUTUAL
`
)

func fromYAML(in string) *unstructured.Unstructured {
	var un unstructured.Unstructured
	if err := yaml.Unmarshal([]byte(in), &un); err != nil {
		panic(err)
	}
	return &un
}

func TestValidateResource(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		valid bool
	}{
		{
			name:  "valid pilot configuration",
			in:    validVirtualService,
			valid: true,
		},
		{
			name:  "invalid pilot configuration",
			in:    invalidVirtualService,
			valid: false,
		},
		{
			name:  "invalid pilot configuration v1beta1",
			in:    invalidVirtualServiceV1Beta1,
			valid: false,
		},
		{
			name:  "valid mixer configuration",
			in:    validMixerRule,
			valid: true,
		},
		{
			name:  "invalid mixer configuration",
			in:    invalidMixerRule,
			valid: false,
		},
		{
			name:  "port name missing service",
			in:    portNameMissingSvc,
			valid: false,
		},
		{
			name:  "version label missing deployment",
			in:    versionLabelMissingDeployment,
			valid: true,
		},
		{
			name:  "valid port naming service",
			in:    validPortNamingSvc,
			valid: true,
		},
		{
			name:  "valid port naming with suffix service",
			in:    validPortNamingWithSuffixSvc,
			valid: true,
		},
		{
			name:  "invalid port naming service",
			in:    invalidPortNamingSvc,
			valid: false,
		},
		{
			name:  "invalid service list",
			in:    invalidSvcList,
			valid: false,
		},
		{
			name:  "valid deployment list",
			in:    validDeploymentList,
			valid: true,
		},
		{
			name:  "skip validating deployment",
			in:    skippedDeployment,
			valid: true,
		},
		{
			name:  "skip validating service",
			in:    skippedService,
			valid: true,
		},
		{
			name:  "service with udp port",
			in:    udpService,
			valid: true,
		},
		{
			name:  "invalid Istio Operator config",
			in:    invalidIstioConfig,
			valid: false,
		},
		{
			name:  "valid Istio Operator config",
			in:    validIstioConfig,
			valid: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v ", i, c.name), func(tt *testing.T) {
			v := &validator{
				mixerValidator: mixervalidate.NewDefaultValidator(false),
			}
			err := v.validateResource("istio-system", fromYAML(c.in))
			if (err == nil) != c.valid {
				tt.Fatalf("unexpected validation result: got %v want %v: err=%v", err == nil, c.valid, err)
			}
		})
	}
}

func TestValidateFiles(t *testing.T) {
	files := []string{
		env.IstioSrc + "/mixer/testdata/config/attributes.yaml",
		env.IstioSrc + "/mixer/template/metric/template.yaml",
		env.IstioSrc + "/mixer/test/prometheus/prometheus-nosession.yaml",
		env.IstioSrc + "/samples/httpbin/policy/keyval-template.yaml",
		env.IstioSrc + "/samples/bookinfo/policy/mixer-rule-deny-ip-crd.yaml",
	}
	istioNamespace := "istio-system"
	b := bytes.Buffer{}
	if err := validateFiles(&istioNamespace, files, true, &b); err != nil {
		t.Fatal(err)
	}
}

func buildMultiDocYAML(docs []string) string {
	var b strings.Builder
	for _, r := range docs {
		if r != "" {
			b.WriteString(strings.Trim(r, " \t\n"))
		}
		b.WriteString("\n---\n")
	}
	return b.String()
}

func createTestFile(t *testing.T, data string) (string, io.Closer) {
	t.Helper()
	validFile, err := ioutil.TempFile("", "TestValidateCommand")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validFile.WriteString(data); err != nil {
		t.Fatal(err)
	}
	return validFile.Name(), validFile
}

func TestValidateCommand(t *testing.T) {
	valid := buildMultiDocYAML([]string{validVirtualService, validVirtualService1})
	invalid := buildMultiDocYAML([]string{invalidVirtualService, validVirtualService1})
	unsupportedMixerRule := buildMultiDocYAML([]string{validVirtualService, validMixerRule})

	validFilename, closeValidFile := createTestFile(t, valid)
	defer closeValidFile.Close()

	invalidFilename, closeInvalidFile := createTestFile(t, invalid)
	defer closeInvalidFile.Close()

	unsupportedMixerRuleFilename, closeMixerRuleFile := createTestFile(t, unsupportedMixerRule)
	defer closeMixerRuleFile.Close()

	invalidYAMLFile, closeInvalidYAMLFile := createTestFile(t, invalidYAML)
	defer closeInvalidYAMLFile.Close()

	validKubernetesYAMLFile, closeKubernetesYAMLFile := createTestFile(t, validKubernetesYAML)
	defer closeKubernetesYAMLFile.Close()

	invalidMixerKindFile, closeInvalidMixerKindFile := createTestFile(t, invalidMixerKind)
	defer closeInvalidMixerKindFile.Close()

	versionLabelMissingDeploymentFile, closeVersionLabelMissingDeploymentFile := createTestFile(t, versionLabelMissingDeployment)
	defer closeVersionLabelMissingDeploymentFile.Close()

	portNameMissingSvcFile, closePortNameMissingSvcFile := createTestFile(t, portNameMissingSvc)
	defer closePortNameMissingSvcFile.Close()

	unsupportedKeyFilename, closeUnsupportedKeyFile := createTestFile(t, invalidUnsupportedKey)
	defer closeUnsupportedKeyFile.Close()

	duplicateKeyFilename, closeUnsupportedKeyFile := createTestFile(t, invalidDuplicateKey)
	defer closeUnsupportedKeyFile.Close()

	validPortNamingSvcFile, closeValidPortNamingSvcFile := createTestFile(t, validPortNamingSvc)
	defer closeValidPortNamingSvcFile.Close()

	validPortNamingWithSuffixSvcFile, closeValidPortNamingWithSuffixSvcFile := createTestFile(t, validPortNamingWithSuffixSvc)
	defer closeValidPortNamingWithSuffixSvcFile.Close()

	invalidPortNamingSvcFile, closeInvalidPortNamingSvcFile := createTestFile(t, invalidPortNamingSvc)
	defer closeInvalidPortNamingSvcFile.Close()

	cases := []struct {
		name           string
		args           []string
		wantError      bool
		expectedRegexp *regexp.Regexp // Expected regexp output
	}{
		{
			name:      "valid port naming service",
			args:      []string{"--filename", validPortNamingSvcFile},
			wantError: false,
		},
		{
			name:      "valid port naming with suffix service",
			args:      []string{"--filename", validPortNamingWithSuffixSvcFile},
			wantError: false,
		},
		{
			name:      "invalid port naming service",
			args:      []string{"--filename", invalidPortNamingSvcFile},
			wantError: true,
		},
		{
			name:      "filename missing",
			wantError: true,
		},
		{
			name: "valid resources from file",
			args: []string{"--filename", validFilename},
		},
		{
			name:      "extra args",
			args:      []string{"--filename", validFilename, "extra-arg"},
			wantError: true,
		},
		{
			name:      "invalid resources from file",
			args:      []string{"--filename", invalidFilename},
			wantError: true,
		},
		{
			name:      "unsupported mixer rule",
			args:      []string{"--filename", unsupportedMixerRuleFilename},
			wantError: true,
		},
		{
			name:      "invalid filename",
			args:      []string{"--filename", "INVALID_FILE_NAME"},
			wantError: true,
		},
		{
			name:      "invalid YAML",
			args:      []string{"--filename", invalidYAMLFile},
			wantError: true,
		},
		{
			name: "valid Kubernetes YAML",
			args: []string{"--filename", validKubernetesYAMLFile},
			expectedRegexp: regexp.MustCompile(`^".*" is valid
$`),
			wantError: false,
		},
		{
			name:      "invalid Mixer kind",
			args:      []string{"--filename", invalidMixerKindFile},
			wantError: true,
		},
		{
			name:           "invalid top-level key",
			args:           []string{"--filename", unsupportedKeyFilename},
			expectedRegexp: regexp.MustCompile(`.*unknown field "unexpected_junk"`),
			wantError:      true,
		},
		{
			name:      "version label missing deployment",
			args:      []string{"--filename", versionLabelMissingDeploymentFile},
			wantError: false,
		},
		{
			name:      "port name missing service",
			args:      []string{"--filename", portNameMissingSvcFile},
			wantError: true,
		},
		{
			name:           "duplicate key",
			args:           []string{"--filename", duplicateKeyFilename},
			expectedRegexp: regexp.MustCompile(`.*key ".*" already set`),
			wantError:      true,
		},
	}
	istioNamespace := "istio-system"
	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v ", i, c.name), func(tt *testing.T) {
			validateCmd := NewValidateCommand(&istioNamespace)
			validateCmd.SetArgs(c.args)

			// capture output to keep test logs clean
			var out bytes.Buffer
			validateCmd.SetOut(&out)
			validateCmd.SetErr(&out)

			err := validateCmd.Execute()
			if (err != nil) != c.wantError {
				tt.Errorf("unexpected validate return status: got %v want %v: \nerr=%v",
					err != nil, c.wantError, err)
			}
			output := out.String()
			if c.expectedRegexp != nil && !c.expectedRegexp.MatchString(output) {
				t.Errorf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v",
					strings.Join(c.args, " "), output, c.expectedRegexp)
			}
		})
	}
}
