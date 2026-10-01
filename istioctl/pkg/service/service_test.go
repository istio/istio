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

package service

import (
	"bytes"
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
)

func TestCreateService(t *testing.T) {
	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{Namespace: "bookinfo"})
	cmd := Cmd(ctx)
	if got, want := cmd.Name(), "sentinel-service"; got != want {
		t.Fatalf("expected command name %q, got %q", want, got)
	}
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{
		"create", "reviews", "--port", "9080", "--selector", "app=reviews",
		"--service-name", "reviews-v2", "--origin-ns", "remote-bookinfo", "--local-ns", "local-bookinfo",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	client, err := ctx.CLIClient()
	if err != nil {
		t.Fatal(err)
	}
	service, err := client.Kube().CoreV1().Services("local-bookinfo").Get(context.Background(), "reviews", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(service.Spec.Ports) != 1 {
		t.Fatalf("expected service with one port, got %v", service.Spec.Ports)
	}
	if got := service.Spec.Ports[0]; got.Port != 9080 || got.TargetPort.IntVal != 9080 {
		t.Fatalf("expected service port and target port 9080, got %v", got)
	}
	if got := service.Spec.Selector["app"]; got != "reviews" {
		t.Fatalf("expected selector app=reviews, got %q", got)
	}
	if got := service.Labels[originalServiceNameLabel]; got != "reviews-v2" {
		t.Fatalf("expected original service name label reviews-v2, got %q", got)
	}
	if got := service.Labels[originalServiceNamespaceLabel]; got != "remote-bookinfo" {
		t.Fatalf("expected original service namespace label remote-bookinfo, got %q", got)
	}
	if got, want := output.String(), "service local-bookinfo/reviews created\n"; got != want {
		t.Fatalf("expected output %q, got %q", want, got)
	}
}
