package status

import (
	"reflect"
	"testing"

	"istio.io/api/meta/v1alpha1"
)

func Test_updateCondition(t *testing.T) {
	type args struct {
		conditions []*v1alpha1.IstioCondition
		condition  *v1alpha1.IstioCondition
	}
	tests := []struct {
		name string
		args args
		want []*v1alpha1.IstioCondition
	}{
		{
			name: "Update conditions",
			args: args{
				conditions: []*v1alpha1.IstioCondition{
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
				condition: &v1alpha1.IstioCondition{
					Type:    "PassedValidation",
					Status:  "False",
					Message: "just a update test, here",
				},
			},
			want: []*v1alpha1.IstioCondition{
				{
					Type:    "PassedValidation",
					Status:  "False",
					Message: "just a update test, here",
				},
				{
					Type:    "Reconciled",
					Status:  "False",
					Message: "1/2 proxies up to date.",
				},
			},
		},
		{
			name: "New conditions",
			args: args{
				conditions: []*v1alpha1.IstioCondition{
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
				condition: &v1alpha1.IstioCondition{
					Type:    "SomeRandomType",
					Status:  "True",
					Message: "just a new condition",
				},
			},
			want: []*v1alpha1.IstioCondition{
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
				{
					Type:    "SomeRandomType",
					Status:  "True",
					Message: "just a new condition",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updateCondition(tt.args.conditions, tt.args.condition); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("updateCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deleteCondition(t *testing.T) {
	type args struct {
		conditions []*v1alpha1.IstioCondition
		condition  string
	}
	tests := []struct {
		name string
		args args
		want []*v1alpha1.IstioCondition
	}{
		{
			name: "Delete conditions",
			args: args{
				conditions: []*v1alpha1.IstioCondition{
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here",
					},
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here2",
					},
					{
						Type:    "Reconciled",
						Status:  "False",
						Message: "1/2 proxies up to date.",
					},
				},
				condition: "PassedValidation",
			},
			want: []*v1alpha1.IstioCondition{
				{
					Type:    "Reconciled",
					Status:  "False",
					Message: "1/2 proxies up to date.",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deleteCondition(tt.args.conditions, tt.args.condition); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deleteCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}
