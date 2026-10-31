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
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/test/util/assert"
)

// TestClientSharesConnection verifies the clients built by one kube.Client share one
// connection pool, although the header timeout disables client-go's transport cache.
func TestClientSharesConnection(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"major":"1","minor":"36","gitVersion":"v1.36.0"}`))
		case "/api":
			_, _ = w.Write([]byte(`{"kind":"APIVersions","versions":["v1"]}`))
		case "/apis":
			_, _ = w.Write([]byte(`{"kind":"APIGroupList","groups":[]}`))
		default:
			_, _ = w.Write([]byte(`{"kind":"List","apiVersion":"v1","metadata":{},"items":[]}`))
		}
	}))
	srv.EnableHTTP2 = true
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})

	c, err := NewClient(NewClientConfigForRestConfig(&rest.Config{Host: srv.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}}), "")
	assert.NoError(t, err)
	ctx := context.Background()
	pods := schema.GroupVersionResource{Version: "v1", Resource: "pods"}

	_, _ = c.Kube().CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	_, _ = c.Dynamic().Resource(pods).List(ctx, metav1.ListOptions{})
	_, _ = c.Metadata().Resource(pods).List(ctx, metav1.ListOptions{})
	_, _ = c.Istio().NetworkingV1().VirtualServices("").List(ctx, metav1.ListOptions{})
	_, _ = c.GetKubernetesVersion()
	_, _ = c.(*client).discoveryClient.ServerGroups()
	cs, err := c.(*client).UtilFactory().KubernetesClientSet()
	assert.NoError(t, err)
	_, _ = cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	dc, err := c.(*client).UtilFactory().DynamicClient()
	assert.NoError(t, err)
	_, _ = dc.Resource(pods).List(ctx, metav1.ListOptions{})
	rc, err := c.(*client).UtilFactory().RESTClient()
	assert.NoError(t, err)
	_ = rc.Get().AbsPath("/api").Do(ctx)

	assert.Equal(t, conns.Load(), int32(1))
}
