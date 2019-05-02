/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package admission

import (
	"net/http"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Func implements an AdmissionReview operation for a GroupVersionResource
type Func func(review v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

// HandleEntry
type admissionHandler struct {
	GVR metav1.GroupVersionResource
	Fn  Func
}

// handle handles an admission request and returns a result
func (ah admissionHandler) handle(review v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	return ah.handle(review)
}

// Manager manages admission controllers
type Manager struct {
	Entries map[string]admissionHandler
	SMux    *http.ServeMux
}

// DefaultAdmissionFns is the default admission control functions registry
var DefaultAdmissionFns = &Manager{
	SMux: http.DefaultServeMux,
}

// HandleFunc registers fn as an admission control webhook callback for the group,version,resources specified
func (e *Manager) HandleFunc(path string, gvr metav1.GroupVersionResource, fn Func) {
	// Register the entry so a Webhook config is created
	e.Entries[path] = admissionHandler{gvr, fn}

	// Register the handler path
	e.SMux.Handle(path, httpHandler{fn})
}

// HandleFunc registers fn as an admission control webhook callback for the group,version,resources specified
func HandleFunc(path string, gvr metav1.GroupVersionResource, fn Func) {
	DefaultAdmissionFns.HandleFunc(path, gvr, fn)
}

// ListenAndServeTLS starts the admission HttpServer.
func ListenAndServeTLS(addr string) error {
	server := &http.Server{
		Addr:      addr,
		TLSConfig: nil, // TODO: Set this
	}
	return server.ListenAndServeTLS("", "")
}
