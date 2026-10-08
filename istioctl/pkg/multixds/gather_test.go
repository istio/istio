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

package multixds

import (
	"context"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/testutil"
	"istio.io/istio/istioctl/pkg/xds"
	"istio.io/istio/pkg/kube"
)

func TestMakeSan(t *testing.T) {
	cases := []struct {
		revision string
		want     string
	}{
		{"", "istiod.istio-system.svc"},
		{"default", "istiod.istio-system.svc"},
		{"canary", "istiod-canary.istio-system.svc"},
	}
	for _, c := range cases {
		if got := makeSan("istio-system", c.revision); got != c.want {
			t.Errorf("makeSan(%q) = %q, want %q", c.revision, got, c.want)
		}
	}
}

func TestSetDefaultSan(t *testing.T) {
	cases := []struct {
		name string
		opts clioptions.CentralControlPlaneOptions
		want string
	}{
		{"ip address", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116:15012"}, "istiod.istio-system.svc"},
		{"localhost", clioptions.CentralControlPlaneOptions{Xds: "localhost:15012"}, "istiod.istio-system.svc"},
		{"ip without port", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116"}, "istiod.istio-system.svc"},
		{"dns name kept", clioptions.CentralControlPlaneOptions{Xds: "istiod.example.com:15012"}, ""},
		{"explicit authority wins", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116:15012", XDSSAN: "custom.san"}, "custom.san"},
		{"insecure skips", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116:15012", InsecureSkipVerify: true}, ""},
		{"plaintext skips", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116:15012", Plaintext: true}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setDefaultSan(&c.opts, "istio-system", kube.NewFakeClient())
			if c.opts.XDSSAN != c.want {
				t.Errorf("XDSSAN = %q, want %q", c.opts.XDSSAN, c.want)
			}
		})
	}
}

func TestQueryEachShardKeepsOptions(t *testing.T) {
	cases := []struct {
		name     string
		opts     clioptions.CentralControlPlaneOptions
		wantSan  string
		insecure bool
	}{
		{"default san", clioptions.CentralControlPlaneOptions{}, "istiod.istio-system.svc", false},
		{"authority kept", clioptions.CentralControlPlaneOptions{XDSSAN: "custom.san"}, "custom.san", false},
		{"insecure kept", clioptions.CentralControlPlaneOptions{InsecureSkipVerify: true}, "istiod.istio-system.svc", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got clioptions.CentralControlPlaneOptions
			GetXdsResponse = func(_ *discovery.DiscoveryRequest, _ string, _ string, opts clioptions.CentralControlPlaneOptions, _ []grpc.DialOption,
			) (*discovery.DiscoveryResponse, error) {
				got = opts
				return &discovery.DiscoveryResponse{}, nil
			}
			defer func() { GetXdsResponse = xds.GetXdsResponse }()

			ctx := cli.NewFakeContext(&cli.NewFakeContextOption{IstioNamespace: "istio-system"})
			client, err := ctx.CLIClient()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Kube().CoreV1().Pods("istio-system").Create(context.TODO(), &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "istiod", Namespace: "istio-system", Labels: map[string]string{"app": "istiod"}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			}, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			if !c.insecure {
				if _, err := client.Kube().CoreV1().ConfigMaps("istio-system").Create(context.TODO(),
					testutil.RootCertConfigMap(t, "istio-system", ""), metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := queryEachShard(true, &discovery.DiscoveryRequest{}, "istio-system", client, c.opts); err != nil {
				t.Fatal(err)
			}
			if got.XDSSAN != c.wantSan {
				t.Errorf("XDSSAN = %q, want %q", got.XDSSAN, c.wantSan)
			}
			if got.InsecureSkipVerify != c.insecure {
				t.Errorf("InsecureSkipVerify = %v, want %v", got.InsecureSkipVerify, c.insecure)
			}
		})
	}
}
