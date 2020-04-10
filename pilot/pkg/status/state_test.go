package status

import (
	"encoding/json"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"reflect"
	"testing"
)

var statusNoDistro = IstioStatus{
	Conditions:         []IstioCondition{{
		Type: HasValidationErrors,
		Status: v1.ConditionTrue,
		Message: "just a test, here",
	}},
	ValidationMessages: []diag.Message{{
		Type:       &diag.MessageType{},
		Parameters: nil,
		Resource:   nil,
		DocRef:     "",
	}},
}

var emptyStatus = map[string]interface{}{}

var statusStillPropagating = IstioStatus{
	Conditions:         []IstioCondition{{
		Type:    HasValidationErrors,
		Status:  v1.ConditionTrue,
		Message: "just a test, here",
	},{
		Type: StillPropagating,
		Status: v1.ConditionTrue,
		Message: "1/2 dataplanes up to date.",
	}},
	ValidationMessages: nil,
}

func TestReconcileStatuses(t *testing.T) {
	type args struct {
		current map[string]interface{}
		desired Fraction
	}
	tests := []struct {
		name  string
		args  args
		want  bool
		want1 *IstioStatus
	}{
		{
			name: "Don't Reconcile when other fields are the only diff",
			args: args{
				current: map[string]interface{}{"status": statusStillPropagating},
				desired: Fraction{1, 2},
			},
			want: false,
		}, {
			name: "Simple Reconcile to true",
			args: args{
				current: map[string]interface{}{"status": statusStillPropagating},
				desired: Fraction{1, 3},
			},
			want: true,
			want1: &IstioStatus{
				Conditions: []IstioCondition{{
					Type:    HasValidationErrors,
					Status:  v1.ConditionTrue,
					Message: "just a test, here",
				}, {
					Type:    StillPropagating,
					Status:  v1.ConditionTrue,
					Message: "1/3 dataplanes up to date.",
				}},
				ValidationMessages: nil,
			},
		}, {
			name: "Simple Reconcile to false",
			args: args{
				current: map[string]interface{}{"status": statusStillPropagating},
				desired: Fraction{2, 2},
			},
			want: true,
			want1: &IstioStatus{
				Conditions: []IstioCondition{{
					Type:    HasValidationErrors,
					Status:  v1.ConditionTrue,
					Message: "just a test, here",
				}, {
					Type:    StillPropagating,
					Status:  v1.ConditionFalse,
					Message: "2/2 dataplanes up to date.",
				}},
				ValidationMessages: nil,
			},
		}, {
			name: "Graceful handling of random status",
			args:args{
				current: map[string]interface{}{"status": "random"},
				desired: Fraction{2, 2},
			},
			want: true,
			want1: &IstioStatus{
				Conditions: []IstioCondition{{
					Type:    StillPropagating,
					Status:  v1.ConditionFalse,
					Message: "2/2 dataplanes up to date.",
				}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := ReconcileStatuses(tt.args.current, tt.args.desired, clock.RealClock{})
			if got != tt.want {
				t.Errorf("ReconcileStatuses() got = %v, want %v", got, tt.want)
			}
			if tt.want1!=nil {
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
	x := IstioStatus{}
	b, _ := json.Marshal(statusStillPropagating)
	_ = json.Unmarshal(b, &x)
	type args struct {
		in interface{}
	}
	tests := []struct {
		name    string
		args    args
		wantOut IstioStatus
		wantErr bool
	}{
		{
			name: "Nondestructive cast",
			args:args{in:statusStillPropagating},
			wantOut:statusStillPropagating,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut, err := getTypedStatus(tt.args.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("getTypedStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotOut, tt.wantOut) {
				t.Errorf("getTypedStatus() gotOut = %v, want %v", gotOut, tt.wantOut)
			}
		})
	}
}