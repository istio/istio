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

	kube_meta "istio.io/istio/galley/pkg/metadata/kube"

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
