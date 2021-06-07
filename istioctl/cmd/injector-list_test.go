package cmd

import (
	"istio.io/api/annotation"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func Test_injectionDisabled(t *testing.T) {
	type args struct {
		pod *v1.Pod
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "disable inject",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
					},
				},
			},
			want: true,
		},
		{
			name: "disable inject, fold equals",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{annotation.SidecarInject.Name: "False"},
					},
				},
			},
			want: true,
		},
		{
			name: "disable inject, fold equals",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{annotation.SidecarInject.Name: "False"},
					},
				},
			},
			want: true,
		},
		{
			name: "empty annotation",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
			},
			want: false,
		},
		{
			name: "nil input",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: nil,
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := injectionDisabled(tt.args.pod); got != tt.want {
				t.Errorf("injectionDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
