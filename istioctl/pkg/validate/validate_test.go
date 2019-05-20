// Copyright 2018 Istio Authors.
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
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	mixervalidate "istio.io/istio/mixer/pkg/validate"
)

const (
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
kind: validator
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
			name:  "valid mixer configuration",
			in:    validMixerRule,
			valid: true,
		},
		{
			name:  "invalid mixer configuration",
			in:    invalidMixerRule,
			valid: false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v ", i, c.name), func(tt *testing.T) {
			v := &validator{
				mixerValidator: mixervalidate.NewDefaultValidator(false),
			}
			err := v.validateResource(fromYAML(c.in))
			if (err == nil) != c.valid {
				tt.Fatalf("unexpected validation result: got %v want %v: err=%q", err == nil, c.valid, err)
			}
		})
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

	unsupportedKeyFilename, closeUnsupportedKeyFile := createTestFile(t, invalidUnsupportedKey)
	defer closeUnsupportedKeyFile.Close()

	cases := []struct {
		name      string
		args      []string
		wantError bool
	}{
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
			name:      "valid Kubernetes YAML",
			args:      []string{"--filename", validKubernetesYAMLFile},
			wantError: false,
		},
		{
			name:      "invalid Mixer kind",
			args:      []string{"--filename", invalidMixerKindFile},
			wantError: true,
		},
		{
			name:      "invalid top-level key",
			args:      []string{"--filename", unsupportedKeyFilename},
			wantError: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v ", i, c.name), func(tt *testing.T) {
			validateCmd := NewValidateCommand()
			validateCmd.SetArgs(c.args)

			// capture output to keep test logs clean
			var out bytes.Buffer
			validateCmd.SetOutput(&out)

			err := validateCmd.Execute()
			if (err != nil) != c.wantError {
				tt.Errorf("unexpected validate return status: got %v want %v: \nerr=%v",
					err != nil, c.wantError, err)
			}
		})
	}
}
