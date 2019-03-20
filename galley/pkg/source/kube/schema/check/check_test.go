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

	kube_meta "istio.io/istio/galley/pkg/metadata/kube"
	sourceSchema "istio.io/istio/galley/pkg/source/kube/schema"

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

	specs := kube_meta.Types.All()

	cases := []struct {
		name    string
		missing map[int]bool
		wantErr bool
	}{
		{
			name:    "all present",
			wantErr: false,
		},
		{
			name: "none ready",
			missing: func() map[int]bool {
				m := make(map[int]bool)
				for i := 0; i < len(specs); i++ {
					m[i] = true
				}
				return m
			}(),
			wantErr: true,
		},
		{
			name:    "first missing",
			missing: map[int]bool{0: true},
			wantErr: true,
		},
		{
			name:    "middle not ready",
			missing: map[int]bool{(len(specs) / 2): true},
			wantErr: true,
		},
		{
			name:    "last not ready",
			missing: map[int]bool{(len(specs) - 1): true},
			wantErr: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			cs := extfake.NewSimpleClientset()

			byGroupVersion := map[string][]meta_v1.APIResource{}
			for i, spec := range specs {
				if c.missing[i] {
					continue
				}
				gv := spec.GroupVersion().String()
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

func TestFindSupportedResourceSchemas(t *testing.T) {
	specs := kube_meta.Types.All()

	cases := []struct {
		name    string
		missing map[int]bool
		want    map[int]bool
	}{
		{
			name: "all present",
		},
		{
			name: "none ready",
			missing: func() map[int]bool {
				m := make(map[int]bool)
				for i := 0; i < len(specs); i++ {
					m[i] = true
				}
				return m
			}(),
		},
		{
			name:    "first missing",
			missing: map[int]bool{0: true},
		},
		{
			name:    "middle not ready",
			missing: map[int]bool{(len(specs) / 2): true},
		},
		{
			name:    "last not ready",
			missing: map[int]bool{(len(specs) - 1): true},
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			cs := extfake.NewSimpleClientset()

			byGroupVersion := map[string][]meta_v1.APIResource{}
			for i, spec := range specs {
				if c.missing[i] {
					continue
				}

				gv := spec.GroupVersion().String()
				byGroupVersion[gv] = append(byGroupVersion[gv], meta_v1.APIResource{Name: spec.Plural})
			}
			for gv, resources := range byGroupVersion {
				resourceList := &meta_v1.APIResourceList{
					GroupVersion: gv,
					APIResources: resources,
				}
				cs.Resources = append(cs.Resources, resourceList)
			}

			var want []sourceSchema.ResourceSpec
			for j, spec := range specs {
				if !c.missing[j] {
					want = append(want, spec)
				}
			}

			got := findSupportedResourceSchemas(cs, specs)

			if len(got) != len(want) {
				tt.Fatalf("wrong number of resource schemas found: \n got %v\nwant %v", got, want)
			}

			for j := range got {
				if got[j].CanonicalResourceName() != want[j].CanonicalResourceName() {
					tt.Fatalf("wrong resource found: got %v want %v", got[j], want[j])
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
	var emptySpecs []sourceSchema.ResourceSpec

	if got := ResourceTypesPresence(m, emptySpecs); got != nil {
		t.Errorf("ResourceTypesPresence() returned unexpected error: %v", got)
	}
	if _, got := FindSupportedResourceSchemas(m, emptySpecs); got != nil {
		t.Errorf("FindSupportedResourceSchemas() returned unexpected error: %v", got)
	}

	m.err = errors.New("oops")
	if got := ResourceTypesPresence(m, emptySpecs); got == nil {
		t.Error("ResourceTypesPresence() returned unexpected success")
	}
	if _, got := FindSupportedResourceSchemas(m, emptySpecs); got == nil {
		t.Errorf("FindSupportedResourceSchemas() returned unexpected success")
	}
}
