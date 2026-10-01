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

package ipaddress

import (
	"bytes"
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
)

func TestCreateIPAddress(t *testing.T) {
	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{Namespace: "local-bookinfo"})
	cmd := Cmd(ctx)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{
		"create", "240.0.0.1",
		"--service-name", "reviews", "--origin-ns", "bookinfo",
		"--local-service-name", "reviews-sentinel",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	client, err := ctx.CLIClient()
	if err != nil {
		t.Fatal(err)
	}
	resource, err := client.Kube().NetworkingV1().IPAddresses().Get(context.Background(), "240.0.0.1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Labels[originalServiceNameLabel] != "reviews" {
		t.Fatalf("expected original service name label, got %v", resource.Labels)
	}
	if resource.Labels[originalServiceNamespaceLabel] != "bookinfo" {
		t.Fatalf("expected original service namespace label, got %v", resource.Labels)
	}
	if resource.Spec.ParentRef == nil || resource.Spec.ParentRef.Resource != "services" ||
		resource.Spec.ParentRef.Namespace != "local-bookinfo" || resource.Spec.ParentRef.Name != "reviews-sentinel" {
		t.Fatalf("unexpected parent reference: %v", resource.Spec.ParentRef)
	}
	if got, want := output.String(), "ipaddress 240.0.0.1 created\n"; got != want {
		t.Fatalf("expected output %q, got %q", want, got)
	}
}

func TestCreateIPAddressWithServiceEntryParent(t *testing.T) {
	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{Namespace: "local-bookinfo"})
	cmd := Cmd(ctx)
	cmd.SetArgs([]string{
		"create", "240.0.0.2",
		"--local-service-entry", "reviews-external",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	client, err := ctx.CLIClient()
	if err != nil {
		t.Fatal(err)
	}
	resource, err := client.Kube().NetworkingV1().IPAddresses().Get(context.Background(), "240.0.0.2", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Spec.ParentRef == nil || resource.Spec.ParentRef.Group != "networking.istio.io" ||
		resource.Spec.ParentRef.Resource != "serviceentries" || resource.Spec.ParentRef.Namespace != "local-bookinfo" ||
		resource.Spec.ParentRef.Name != "reviews-external" {
		t.Fatalf("unexpected parent reference: %v", resource.Spec.ParentRef)
	}
}

func TestCreateIPAddressRejectsMultipleParents(t *testing.T) {
	cmd := Cmd(cli.NewFakeContext(nil))
	cmd.SetArgs([]string{
		"create", "240.0.0.3",
		"--local-service-name", "reviews",
		"--local-service-entry", "reviews-external",
	})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected conflicting parent flags to fail")
	}
}
