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

package xds

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
)

func rootCertConfigMap(t *testing.T, ns, data string) *corev1.ConfigMap {
	t.Helper()
	if data == "" {
		pem, err := os.ReadFile(filepath.Join(env.IstioSrc, "tests/testdata/certs/pilot/root-cert.pem"))
		if err != nil {
			t.Fatal(err)
		}
		data = string(pem)
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: controller.CACertNamespaceConfigMap, Namespace: ns},
		Data:       map[string]string{constants.CACertNamespaceConfigMapDataName: data},
	}
}

func TestTLSConfig(t *testing.T) {
	const ns = "istio-system"
	cases := []struct {
		name           string
		opts           clioptions.CentralControlPlaneOptions
		objects        []runtime.Object
		wantServerName string
		wantInsecure   bool
		wantErr        bool
	}{
		{
			name:           "server name from authority",
			opts:           clioptions.CentralControlPlaneOptions{XDSSAN: "istiod.istio-system.svc", Xds: "localhost:15012"},
			objects:        []runtime.Object{rootCertConfigMap(t, ns, "")},
			wantServerName: "istiod.istio-system.svc",
		},
		{
			name:           "server name from xds address",
			opts:           clioptions.CentralControlPlaneOptions{Xds: "istiod.example.com:15012"},
			objects:        []runtime.Object{rootCertConfigMap(t, ns, "")},
			wantServerName: "istiod.example.com",
		},
		{
			name:           "insecure does not need the root cert",
			opts:           clioptions.CentralControlPlaneOptions{Xds: "localhost:15012", InsecureSkipVerify: true},
			wantServerName: "localhost",
			wantInsecure:   true,
		},
		{
			name:    "missing configmap fails closed",
			opts:    clioptions.CentralControlPlaneOptions{XDSSAN: "istiod.istio-system.svc"},
			wantErr: true,
		},
		{
			name:    "configmap in another namespace fails closed",
			opts:    clioptions.CentralControlPlaneOptions{XDSSAN: "istiod.istio-system.svc"},
			objects: []runtime.Object{rootCertConfigMap(t, "default", "")},
			wantErr: true,
		},
		{
			name:    "invalid root cert fails closed",
			opts:    clioptions.CentralControlPlaneOptions{XDSSAN: "istiod.istio-system.svc"},
			objects: []runtime.Object{rootCertConfigMap(t, ns, "not a cert")},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := kube.NewFakeClient(tc.objects...)
			got, err := tlsConfig(context.Background(), tc.opts, ns, client)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got config %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ServerName != tc.wantServerName {
				t.Errorf("ServerName = %q, want %q", got.ServerName, tc.wantServerName)
			}
			if got.InsecureSkipVerify != tc.wantInsecure {
				t.Errorf("InsecureSkipVerify = %v, want %v", got.InsecureSkipVerify, tc.wantInsecure)
			}
			if !tc.wantInsecure && got.RootCAs == nil {
				t.Error("RootCAs not set")
			}
		})
	}
}
