//  Copyright 2019 Istio Authors
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

package processing

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	mcp "istio.io/api/mcp/v1alpha1"

	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestSnapshotter_Basic(t *testing.T) {
	ev1 := NewStoredProjection(emptyInfo.TypeURL)
	ev1.Set(res1V1().ID.FullName, toMcpResourceOrPanic(res1V1()))
	ev1.Set(res2V1().ID.FullName, toMcpResourceOrPanic(res2V1()))

	ev2 := NewStoredProjection(structInfo.TypeURL)
	ev2.Set(res3V1().ID.FullName, toMcpResourceOrPanic(res3V1()))

	views := []Projection{ev1, ev2}
	s := newSnapshotter(views)

	sn := s.snapshot([]resource.TypeURL{emptyInfo.TypeURL})

	expected := []*mcp.Resource{toMcpResourceOrPanic(res1V1()), toMcpResourceOrPanic(res2V1())}

	actual := sn.Resources(emptyInfo.TypeURL.String())
	sort.Slice(actual, func(i, j int) bool {
		return strings.Compare(actual[i].Metadata.Name, actual[j].Metadata.Name) < 0
	})
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch: got:%v, wanted:%v", actual, expected)
	}

	// Use the other type
	sn = s.snapshot([]resource.TypeURL{structInfo.TypeURL})
	actual = sn.Resources(structInfo.TypeURL.String())

	expected = []*mcp.Resource{toMcpResourceOrPanic(res3V1())}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch: got:%v, wanted:%v", actual, expected)
	}
}

func TestSnapshotter_MultiView(t *testing.T) {
	ev1 := NewStoredProjection(emptyInfo.TypeURL)
	ev1.Set(res1V1().ID.FullName, toMcpResourceOrPanic(res1V1()))

	ev2 := NewStoredProjection(emptyInfo.TypeURL)
	ev2.Set(res2V1().ID.FullName, toMcpResourceOrPanic(res2V1()))

	views := []Projection{ev1, ev2}
	s := newSnapshotter(views)

	sn := s.snapshot([]resource.TypeURL{emptyInfo.TypeURL})

	expected := []*mcp.Resource{toMcpResourceOrPanic(res1V1()), toMcpResourceOrPanic(res2V1())}

	actual := sn.Resources(emptyInfo.TypeURL.String())
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Mismatch: got:%v, wanted:%v", actual, expected)
	}
}
