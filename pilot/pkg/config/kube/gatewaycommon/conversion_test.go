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

package gatewaycommon

import (
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestHumanReadableJoin(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b, and c"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.input, "_"), func(t *testing.T) {
			if got := HumanReadableJoin(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPruneListenerSetStatusListeners(t *testing.T) {
	obj := &gatewayv1.ListenerSet{
		Spec: gatewayv1.ListenerSetSpec{
			Listeners: []gatewayv1.ListenerEntry{
				{Name: "a"},
				{Name: "b"},
			},
		},
	}
	status := &gatewayv1.ListenerSetStatus{
		Listeners: []gatewayv1.ListenerEntryStatus{
			{Name: "a"},
			{Name: "b"},
			{Name: "stale"},
		},
	}

	PruneListenerSetStatusListeners(obj, status)

	if len(status.Listeners) != 2 {
		t.Fatalf("len(status.Listeners) = %d, want 2", len(status.Listeners))
	}
	if status.Listeners[0].Name != "a" || status.Listeners[1].Name != "b" {
		t.Fatalf("unexpected listeners: %+v", status.Listeners)
	}
}

func TestReportUnsupportedListenerSet(t *testing.T) {
	obj := &gatewayv1.ListenerSet{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	status := &gatewayv1.ListenerSetStatus{
		Listeners: []gatewayv1.ListenerEntryStatus{{Name: "x"}},
	}

	ReportUnsupportedListenerSet("example", status, obj)

	if len(status.Listeners) != 0 {
		t.Fatalf("expected listeners cleared, got %+v", status.Listeners)
	}
	accepted := findCondition(status.Conditions, string(gatewayv1.GatewayConditionAccepted))
	if accepted == nil || accepted.Status != metav1.ConditionFalse {
		t.Fatalf("Accepted = %+v, want False", accepted)
	}
	if accepted.Reason != string(gatewayv1.ListenerSetReasonNotAllowed) {
		t.Fatalf("Accepted reason = %q", accepted.Reason)
	}
}

func findCondition(conditions []metav1.Condition, typ string) *metav1.Condition {
	for _, c := range conditions {
		if c.Type == typ {
			return &c
		}
	}
	return nil
}
