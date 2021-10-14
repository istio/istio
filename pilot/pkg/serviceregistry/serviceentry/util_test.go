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

package serviceentry

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestGetWorkloadServiceEntries(t *testing.T) {
	ses := []config.Config{
		{
			Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Namespace: "default", Name: "se-1"},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"*.google.com"},
				Ports: []*networking.Port{
					{Number: 80, Name: "http-number", Protocol: "http"},
					{Number: 8080, Name: "http2-number", Protocol: "http2"},
				},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: map[string]string{"app": "foo"},
				},
			},
		},
		{
			Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Namespace: "default", Name: "se-2"},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"*.google.com"},
				Ports: []*networking.Port{
					{Number: 80, Name: "http-number", Protocol: "http"},
					{Number: 8080, Name: "http2-number", Protocol: "http2"},
				},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: map[string]string{"app": "bar"},
				},
			},
		},
		{
			Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Namespace: "default", Name: "se-3"},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"www.wikipedia.org"},
				Ports: []*networking.Port{
					{Number: 80, Name: "http-number", Protocol: "http"},
					{Number: 8080, Name: "http2-number", Protocol: "http2"},
				},
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: map[string]string{"app": "foo"},
				},
			},
		},
	}

	wle := &networking.WorkloadEntry{
		Address: "2.3.4.5",
		Labels: map[string]string{
			"app":     "foo",
			"version": "v1",
		},
		Ports: map[string]uint32{
			"http-number":  8081,
			"http2-number": 8088,
		},
	}

	expected := map[types.NamespacedName]struct{}{
		{Namespace: "default", Name: "se-1"}: {},
		{Namespace: "default", Name: "se-3"}: {},
	}
	got := getWorkloadServiceEntries(ses, wle)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("recv unexpected se: %v", got)
	}
}

func TestCompareServiceEntries(t *testing.T) {
	oldSes := []types.NamespacedName{
		{Namespace: "default", Name: "se-1"},
		{Namespace: "default", Name: "se-2"},
		{Namespace: "default", Name: "se-3"},
	}
	currSes := map[types.NamespacedName]struct{}{
		{Namespace: "default", Name: "se-2"}: {},
		{Namespace: "default", Name: "se-4"}: {},
		{Namespace: "default", Name: "se-5"}: {},
	}

	expectedNew := map[types.NamespacedName]struct{}{
		{Namespace: "default", Name: "se-4"}: {},
		{Namespace: "default", Name: "se-5"}: {},
	}
	expectedDeselect := map[types.NamespacedName]struct{}{
		{Namespace: "default", Name: "se-1"}: {},
		{Namespace: "default", Name: "se-3"}: {},
	}
	expectedUnchanged := map[types.NamespacedName]struct{}{
		{Namespace: "default", Name: "se-2"}: {},
	}
	newSelected, unSelected, unchanged := compareServiceEntries(oldSes, currSes)
	if len(newSelected) != len(expectedNew) {
		t.Errorf("got unexpected newSelected ses %v", newSelected)
	}
	for _, se := range newSelected {
		if _, ok := expectedNew[se]; !ok {
			t.Errorf("got unexpected newSelected se %v", se)
		}
	}

	if len(unSelected) != len(expectedDeselect) {
		t.Errorf("got unexpected unSelected ses %v", unSelected)
	}
	for _, se := range unSelected {
		if _, ok := expectedDeselect[se]; !ok {
			t.Errorf("got unexpected unSelected se %v", se)
		}
	}

	if len(unchanged) != len(expectedUnchanged) {
		t.Errorf("got unexpected unchanged ses %v", unchanged)
	}
	for _, se := range unchanged {
		if _, ok := expectedUnchanged[se]; !ok {
			t.Errorf("got unexpected unchanged se %v", se)
		}
	}
}
