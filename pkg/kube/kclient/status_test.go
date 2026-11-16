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

package kclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackingBody struct {
	io.Reader
	read   bool
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) {
	b.read = true
	return b.Reader.Read(p)
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func TestUpdateStatusNoResponse(t *testing.T) {
	responseBody := &trackingBody{Reader: strings.NewReader(`{"kind":"Pod"}`)}
	httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPut {
			t.Fatalf("method: got %q, want PUT", request.Method)
		}
		if request.URL.Path != "/api/v1/namespaces/ns/pods/pod/status" {
			t.Fatalf("path: got %q", request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content type: got %q", request.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(request.Body)
		want := `{"kind":"Pod","apiVersion":"v1","metadata":{"name":"pod","namespace":"ns"},"spec":{"containers":null},"status":{}}`
		if string(body) != want {
			t.Fatalf("body: got %s, want %s", body, want)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       responseBody,
			Request:    request,
		}, nil
	})}
	config := &rest.Config{
		Host:                 "https://example.test",
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
	}
	client, err := rest.UnversionedRESTClientForConfigAndClient(config, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	err = updateStatusNoResponse(context.Background(), client, schema.GroupVersionResource{
		Version:  "v1",
		Resource: "pods",
	}, schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns"}})
	if err != nil {
		t.Fatal(err)
	}
	if responseBody.read {
		t.Fatal("successful response body was read")
	}
	if !responseBody.closed {
		t.Fatal("successful response body was not closed")
	}
}

func TestUpdateStatusNoResponseReturnsAPIError(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"kind":"Status",
				"apiVersion":"v1",
				"status":"Failure",
				"reason":"Conflict",
				"code":409
			}`)),
			Request: request,
		}, nil
	})}
	config := &rest.Config{
		Host:                 "https://example.test",
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
	}
	client, err := rest.UnversionedRESTClientForConfigAndClient(config, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	err = updateStatusNoResponse(context.Background(), client, schema.GroupVersionResource{
		Group:    "example.io",
		Version:  "v1",
		Resource: "examples",
	}, schema.GroupVersionKind{Group: "example.io", Version: "v1", Kind: "Example"}, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "ns"}})
	if !apierrors.IsConflict(err) {
		t.Fatalf("got %v, want conflict", err)
	}
}
