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

package check

import (
	"fmt"
	"testing"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"

	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCheckCRDPresence(t *testing.T) {
	prevInterval, prevTimeout := pollInterval, pollTimeout
	pollInterval = time.Nanosecond
	pollTimeout = time.Millisecond
	defer func() {
		pollInterval, pollTimeout = prevInterval, prevTimeout
	}()

	k8sSpecs := k8smeta.MustGet().KubeSource().Resources()
	testSpecs := basicmeta.MustGet2().KubeSource().Resources()
	specs := append(k8sSpecs, testSpecs...)

	cases := []struct {
		name    string
		missing map[string]bool
		wantErr bool
	}{
		{
			name:    "all present",
			wantErr: false,
		},
		{
			name: "none ready",
			missing: func() map[string]bool {
				m := make(map[string]bool)
				for _, spec := range specs {
					m[spec.Plural] = true
				}
				return m
			}(),
			wantErr: true,
		},
		{
			name:    "first missing",
			missing: map[string]bool{"Kind1s": true},
			wantErr: true,
		},
		{
			name:    "pod not ready",
			missing: map[string]bool{"pods": true},
			wantErr: true,
		},
		{
			name:    "optional not ready",
			missing: map[string]bool{"Kind2s": true},
			wantErr: false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			cs := extfake.NewSimpleClientset()

			byGroupVersion := map[string][]meta_v1.APIResource{}
			for _, spec := range specs {
				if c.missing[spec.Plural] {
					continue
				}
				gv := meta_v1.GroupVersion{Group: spec.Group, Version: spec.Version}.String()
				byGroupVersion[gv] = append(byGroupVersion[gv], meta_v1.APIResource{Name: spec.Plural})
			}
			for gv, resources := range byGroupVersion {
				resourceList := &meta_v1.APIResourceList{
					GroupVersion: gv,
					APIResources: resources,
				}
				cs.Resources = append(cs.Resources, resourceList)
			}

			err := resourceTypesPresence(cs, specs)
			if c.wantErr {
				if err == nil {
					tt.Fatal("expected error but got success")
				}
			} else {
				if err != nil {
					tt.Fatalf("expected success but got error: %v", err)
				}
			}
		})
	}
}

type mockExtensionClient struct{ err error }

func (m mockExtensionClient) DynamicInterface() (dynamic.Interface, error)         { return nil, m.err }
func (m mockExtensionClient) APIExtensionsClientset() (clientset.Interface, error) { return nil, m.err }
func (m mockExtensionClient) KubeClient() (kubernetes.Interface, error)            { return nil, m.err }

func TestExportedFunctions(t *testing.T) {
	var m mockExtensionClient

	// provide an empty list of specs so the calling code doesn't
	// invoke the mockExtensionClient's unimplemented discovery API. Those
	// functions are tested covered by TestCheckCRDPresence and TestFindSupportedResourceSchemas
	var emptySpecs []schema.KubeResource

	if got := ResourceTypesPresence(m, emptySpecs); got != nil {
		t.Errorf("ResourceTypesPresence() returned unexpected error: %v", got)
	}

	m.err = errors.New("oops")
	if got := ResourceTypesPresence(m, emptySpecs); got == nil {
		t.Error("ResourceTypesPresence() returned unexpected success")
	}
}
