// Copyright 2019 Istio Authors
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

package main

import (
	"github.com/spf13/cobra"
	"reflect"
	"testing"

	"istio.io/istio/security/pkg/pki/ca"
	"k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

func Test_createCA(t *testing.T) {
	client := fake.NewSimpleClientset()
	type args struct {
		client corev1.CoreV1Interface
	}
	tests := []struct {
		name     string
		args     args
		commands []*cobra.Command
		want     *ca.IstioCA
	}{
		{
			name: "Given a client with no TrustDomain specified",
			args: args{
				client: client.CoreV1(),
			},
			want: &ca.IstioCA{},
		},
		{
			name: "Given a client with a custom TrustDomain specified",
			args: args{
				client: client.CoreV1(),
			},
			want: &ca.IstioCA{},
		},
	}
	for _, tt := range tests {
		rootCmd.ResetCommands()
		rootCmd.AddCommand(tt.commands...)
		t.Run(tt.name, func(t *testing.T) {
			if got := createCA(tt.args.client); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("createCA() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
