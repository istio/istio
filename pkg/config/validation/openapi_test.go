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

package validation_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	crdvalidation "istio.io/istio/pkg/config/crd"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/fuzz"
	"istio.io/istio/pkg/test/util/yml"
)

type TestExpectation struct {
	WantErr string `json:"_err,omitempty"`
}

func FuzzCRDs(f *testing.F) {
	v := crdvalidation.NewIstioValidator(f)
	base := "testdata/crds"
	d, err := os.ReadDir(base)
	if err != nil {
		f.Fatal(err)
	}
	for _, ff := range d {
		fc, err := os.ReadFile(filepath.Join(base, ff.Name()))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(fc))
	}
	f.Fuzz(func(t *testing.T, data string) {
		t.Log("Running", data)
		defer fuzz.Finalize()
		compareCRDandWebhookValidation(t, true, data, v)
	})
}

func TestCRDs(t *testing.T) {
	v := crdvalidation.NewIstioValidator(t)
	base := "testdata/crds"
	d, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range d {
		t.Run(f.Name(), func(t *testing.T) {
			f, err := os.ReadFile(filepath.Join(base, f.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range yml.SplitString(string(f)) {
				compareCRDandWebhookValidation(t, false, item, v)
			}
		})
	}
}

func compareCRDandWebhookValidation(t *testing.T, fuzz bool, item string, v *crdvalidation.Validator) {
	parseError := t.Fatal
	if fuzz {
		// Invalid input is skipped for fuzzer
		parseError = t.Skip
	}
	openAPI := validateOpenAPI(parseError, item, v)
	name, webhook := validateWebhook(parseError, item)

	want := TestExpectation{}
	if err := yaml.Unmarshal([]byte(item), &want); err != nil {
		parseError(err)
	}

	t.Run(name, func(t *testing.T) {
		if strings.Contains(want.WantErr, "should be at least 1 chars long") {
			t.Skip("webhook cannot handle '' vs unset")
		}
		if openAPI != nil && nilError.MatchString(openAPI.Error()) {
			// Kubernetes will reject something like `foo: null`. We cannot distinguish this in the webhook at all.
			t.Skip(openAPI)
		}
		if openAPI != nil && intEnumError.MatchString(openAPI.Error()) {
			// CRD only allows string enums. Webhook allows ints, and we cannot really change this.
			t.Skip(openAPI)
		}
		if openAPI != nil && strings.Contains(openAPI.Error(), "unknown fields") {
			// We cannot reliably do this in webhook and it not really testing the webhook anyways
			// Even strict mode in json processing is not enough as it is case insensitive.
			t.Skip(openAPI)
		}
		if webhook != nil && strings.Contains(webhook.Error(), "jwks parse error") {
			// We cannot parse this in CEL yet
			t.Skip(webhook)
		}
		if (openAPI == nil) != (webhook == nil) {
			t.Fatalf("mismatch:\ncrd    : %v\nwebhook: %v", openAPI, webhook)
		}
	})
}

func validateWebhook(parseError func(args ...any), item string) (string, error) {
	var u map[string]any
	if err := yaml.Unmarshal([]byte(item), &u); err != nil {
		parseError(err)
	}
	delete(u, "_err")
	by, err := yaml.Marshal(u)
	if err != nil {
		parseError(err)
	}
	var obj crd.IstioKind
	if err := yaml.UnmarshalStrict(by, &obj); err != nil {
		parseError(err)
	}

	gvk := obj.GroupVersionKind()
	s, exists := collections.All.FindByGroupVersionAliasesKind(resource.FromKubernetesGVK(&gvk))
	if !exists {
		parseError(fmt.Sprintf("unrecognized type: %v", gvk))
	}
	out, err := crd.ConvertObject(s, &obj, "cluster.local")
	if err != nil {
		if strings.Contains(err.Error(), "bad Duration") ||
			strings.Contains(err.Error(), "unknown value") ||
			strings.Contains(err.Error(), "invalid character") {
			// These are basically validation errors, but webhook captures them in the 'wrong' place. Treat as non-parse error
			return "", err
		}
		parseError(fmt.Sprintf("convert: %v", err))
	}
	_, e := s.ValidateConfig(*out)
	return obj.Name, e
}

var (
	nilError     = regexp.MustCompile(`in body must be of type .+?: "null"`)
	intEnumError = regexp.MustCompile(`in body must be of type string: "integer", .+?: Unsupported value: .+?: supported values:`)
)

func validateOpenAPI(parseError func(args ...any), item string, v *crdvalidation.Validator) error {
	us := &unstructured.Unstructured{}
	if err := yaml.Unmarshal([]byte(item), us); err != nil {
		parseError(err)
	}
	delete(us.Object, "_err")
	// Parse early so we skip for fuzzing instead of failing
	gvk := us.GroupVersionKind()
	_, exists := collections.All.FindByGroupVersionAliasesKind(resource.FromKubernetesGVK(&gvk))
	if !exists {
		parseError(fmt.Sprintf("unrecognized type: %v", gvk))
	}
	for k := range us.Object {
		if k != "spec" && strings.EqualFold(k, "spec") {
			parseError(k)
		}
	}
	return v.ValidateCustomResource(us)
}
