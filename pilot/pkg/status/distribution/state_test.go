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

package distribution

import (
	"encoding/json"
	"reflect"
	"testing"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config"
)

var statusStillPropagating = &v1alpha1.IstioStatus{
	Conditions: []*v1alpha1.IstioCondition{
		{
			Type:    "PassedValidation",
			Status:  "True",
			Message: "just a test, here",
		},
		{
			Type:    "Reconciled",
			Status:  "False",
			Message: "1/2 proxies up to date.",
		},
	},
	ValidationMessages: nil,
}

func TestReconcileStatuses(t *testing.T) {
	type args struct {
		current *config.Config
		desired Progress
	}
	tests := []struct {
		name  string
		args  args
		want  bool
		want1 *v1alpha1.IstioStatus
	}{
		{
			name: "Don't Reconcile when other fields are the only diff",
			args: args{
				current: &config.Config{Status: statusStillPropagating},
				desired: Progress{1, 2},
			},
			want: false,
		}, {
			name: "Simple Reconcile to true",
			args: args{
				current: &config.Config{Status: statusStillPropagating},
				desired: Progress{1, 3},
			},
			want: true,
			want1: &v1alpha1.IstioStatus{
				Conditions: []*v1alpha1.IstioCondition{
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here",
					},
					{
						Type:    "Reconciled",
						Status:  "False",
						Message: "1/3 proxies up to date.",
					},
				},
				ValidationMessages: nil,
			},
		}, {
			name: "Simple Reconcile to false",
			args: args{
				current: &config.Config{Status: statusStillPropagating},
				desired: Progress{2, 2},
			},
			want: true,
			want1: &v1alpha1.IstioStatus{
				Conditions: []*v1alpha1.IstioCondition{
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here",
					},
					{
						Type:    "Reconciled",
						Status:  "True",
						Message: "2/2 proxies up to date.",
					},
				},
				ValidationMessages: nil,
			},
		}, {
			name: "Reconcile for message difference",
			args: args{
				current: &config.Config{Status: statusStillPropagating},
				desired: Progress{2, 3},
			},
			want: true,
			want1: &v1alpha1.IstioStatus{
				Conditions: []*v1alpha1.IstioCondition{
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here",
					},
					{
						Type:    "Reconciled",
						Status:  "False",
						Message: "2/3 proxies up to date.",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := ReconcileStatuses(tt.args.current.Status.(*v1alpha1.IstioStatus), tt.args.desired)
			if got != tt.want {
				t.Errorf("ReconcileStatuses() got = %v, want %v", got, tt.want)
			}
			if tt.want1 != nil {
				for i := range tt.want1.Conditions {
					if got1 != nil && i < len(got1.Conditions) {
						tt.want1.Conditions[i].LastTransitionTime = got1.Conditions[i].LastTransitionTime
						tt.want1.Conditions[i].LastProbeTime = got1.Conditions[i].LastProbeTime
					}
				}
				if !reflect.DeepEqual(got1, tt.want1) {
					t.Errorf("ReconcileStatuses() got1 = %v, want %v", got1, tt.want1)
				}
			}
		})
	}
}

func Test_getTypedStatus(t *testing.T) {
	x := v1alpha1.IstioStatus{}
	b, _ := json.Marshal(statusStillPropagating)
	_ = json.Unmarshal(b, &x)
	type args struct {
		in any
	}
	tests := []struct {
		name    string
		args    args
		wantOut *v1alpha1.IstioStatus
		wantErr bool
	}{
		{
			name:    "Nondestructive cast",
			args:    args{in: statusStillPropagating},
			wantOut: statusStillPropagating,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut, err := status.GetTypedStatus(tt.args.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTypedStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotOut, tt.wantOut) {
				t.Errorf("GetTypedStatus() gotOut = %v, want %v", gotOut, tt.wantOut)
			}
		})
	}
}
