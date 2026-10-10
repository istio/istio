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

package kube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/envoy/admin"
)

func Test_findIstiodMonitoringPort(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected int
	}{
		{
			name: "Annotation exists",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"prometheus.io/port": "15016",
					},
				},
			},
			expected: 15016,
		},
		{
			name: "No monitoringAddr",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "discovery",
						},
					},
				},
			},
			expected: 15014, // Default value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := FindIstiodMonitoringPort(tt.pod)
			if actual != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, actual)
			}
		})
	}
}

func TestEnvoyAdminCommand(t *testing.T) {
	for _, tt := range []struct {
		name, transport string
		native          bool
		port            int
		wantExec, fail  bool
	}{
		{name: "legacy TCP", port: 15000},
		{name: "explicit TCP", transport: "TCP", port: 15000},
		{name: "native TCP", native: true, transport: "TCP", port: 15000},
		{name: "native UDS", native: true, transport: "UDS", port: 15000, wantExec: true},
		{name: "container UDS discovery", transport: "UDS", port: 15000, wantExec: true},
		{name: "agent debug", native: true, transport: "UDS", port: 15020},
		{name: "alternate admin port", native: true, transport: "UDS", port: 12345, fail: true},
		{name: "invalid transport", native: true, transport: "invalid", port: 15000, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			container := corev1.Container{Name: "istio-proxy"}
			if tt.transport != "" {
				container.Env = []corev1.EnvVar{{Name: admin.Env, Value: tt.transport}}
			}
			pod := &corev1.Pod{}
			if tt.native {
				pod.Spec.InitContainers = []corev1.Container{container}
			} else {
				pod.Spec.Containers = []corev1.Container{container}
			}
			path := "logging?level=debug&name=$(echo unsafe)"
			got, err := envoyAdminCommand(pod, "POST", path, tt.port)
			if (err != nil) != tt.fail {
				t.Fatalf("got %v, want failure %v", err, tt.fail)
			}
			if tt.wantExec {
				want := []string{"pilot-agent", "request", "POST", path}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %v, want %v", got, want)
				}
			} else if got != nil {
				t.Fatalf("unexpected exec %v", got)
			}
		})
	}
}

func TestAdminExecDenied(t *testing.T) {
	var execCalls, otherCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/exec") {
			execCalls.Add(1)
			if got := r.URL.Query()["command"]; !reflect.DeepEqual(got, []string{"pilot-agent", "request", "GET", "config_dump"}) {
				t.Errorf("exec command: %v", got)
			}
			http.Error(w, "pods/exec forbidden", http.StatusForbidden)
			return
		}
		otherCalls.Add(1)
		http.Error(w, "unexpected fallback", http.StatusInternalServerError)
	}))
	defer server.Close()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "proxy", Namespace: "test"}, Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "istio-proxy", Env: []corev1.EnvVar{{Name: admin.Env, Value: "UDS"}}}}}}
	c := NewFakeClient(pod).(*client)
	cfg := &rest.Config{Host: server.URL, ContentConfig: rest.ContentConfig{GroupVersion: &schema.GroupVersion{Version: "v1"}, NegotiatedSerializer: kubescheme.Codecs.WithoutConversion()}, APIPath: "/api"}
	var err error
	c.config = cfg
	c.restClient, err = rest.RESTClientFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.EnvoyDoWithPort(context.Background(), "proxy", "test", "GET", "config_dump", 15000)
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected exec RBAC error, got %v", err)
	}
	if execCalls.Load() == 0 || otherCalls.Load() != 0 {
		t.Fatalf("exec calls %d, other calls %d", execCalls.Load(), otherCalls.Load())
	}
}

func TestAdminExecCancellation(t *testing.T) {
	for _, handshake := range []bool{false, true} {
		t.Run(fmt.Sprintf("during-handshake=%v", handshake), func(t *testing.T) {
			release := make(chan struct{})

			entered := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if handshake {
					entered <- struct{}{}
					<-release
					return
				}
				upgrader := websocket.Upgrader{Subprotocols: []string{"v5.channel.k8s.io"}}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Errorf("upgrade exec: %v", err)
					return
				}
				defer conn.Close()
				entered <- struct{}{}
				// Read until cancellation closes the upgraded stream.
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			defer close(release)
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "proxy", Namespace: "test"}, Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "istio-proxy", Env: []corev1.EnvVar{{Name: admin.Env, Value: "UDS"}}}}}}
			c := NewFakeClient(pod).(*client)
			cfg := &rest.Config{Host: server.URL, ContentConfig: rest.ContentConfig{GroupVersion: &schema.GroupVersion{Version: "v1"}, NegotiatedSerializer: kubescheme.Codecs.WithoutConversion()}, APIPath: "/api"}
			var err error
			c.config = cfg
			c.restClient, err = rest.RESTClientFor(cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := c.EnvoyDoWithPort(ctx, "proxy", "test", "GET", "config_dump", 15000); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("exec request did not reach server")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected cancellation, got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("exec ignored cancellation")
			}

		})
	}
}
