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

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/hashicorp/go-multierror"
	kubeApiAdmission "k8s.io/api/admission/v1"
	kubeApiApps "k8s.io/api/apps/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
)

var scope = log.RegisterScope("validationServer", "validation webhook server", 0)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()

	// Expect AdmissionRequest to only include these top-level field names
	validFields = map[string]bool{
		"apiVersion": true,
		"kind":       true,
		"metadata":   true,
		"spec":       true,
		"status":     true,
	}
)

func init() {
	_ = kubeApiApps.AddToScheme(runtimeScheme)
}

// Options contains the configuration for the Istio Pilot validation
// admission controller.
type Options struct {
	// Schemas provides a description of all configuration resources.
	Schemas collection.Schemas

	// DomainSuffix is the DNS domain suffix for Pilot CRD resources,
	// e.g. cluster.local.
	DomainSuffix string

	// Port where the webhook is served. the number should be greater than 1024 for non-root
	// user, because non-root user cannot bind port number less than 1024
	// Mainly used for testing. Webhook server is started by Istiod.
	Port uint

	// Use an existing mux instead of creating our own.
	Mux *http.ServeMux
}

// String produces a stringified version of the arguments for debugging.
func (o Options) String() string {
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, "DomainSuffix: %s\n", o.DomainSuffix)
	_, _ = fmt.Fprintf(buf, "Port: %d\n", o.Port)

	return buf.String()
}

// DefaultArgs allocates an Options struct initialized with Webhook's default configuration.
func DefaultArgs() Options {
	return Options{
		Port: 9443,
	}
}

// Webhook implements the validating admission webhook for validating Istio configuration.
type Webhook struct {
	// pilot
	schemas      collection.Schemas
	domainSuffix string
}

// New creates a new instance of the admission webhook server.
func New(p Options) (*Webhook, error) {
	if p.Mux == nil {
		scope.Error("mux not set correctly")
		return nil, errors.New("expected mux to be passed, but was not passed")
	}
	wh := &Webhook{
		schemas: p.Schemas,
	}

	p.Mux.HandleFunc("/validate", wh.serveValidate)
	// old handlers retained backwards compatibility during upgrades
	p.Mux.HandleFunc("/admitpilot", wh.serveAdmitPilot)

	return wh, nil
}

//Stop the server
func (wh *Webhook) Stop() {
}

var readyHook = func() {}

// Run implements the webhook server
func (wh *Webhook) Run(stopCh <-chan struct{}) {

	defer func() {
		wh.Stop()
	}()

	if readyHook != nil {
		readyHook()
	}
}

func toAdmissionResponse(err error) *kubeApiAdmission.AdmissionResponse {
	return &kubeApiAdmission.AdmissionResponse{Result: &metav1.Status{Message: err.Error()}}
}

type admitFunc func(*kubeApiAdmission.AdmissionRequest) *kubeApiAdmission.AdmissionResponse

func serve(w http.ResponseWriter, r *http.Request, admit admitFunc) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		reportValidationHTTPError(http.StatusBadRequest)
		http.Error(w, "no body found", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		reportValidationHTTPError(http.StatusUnsupportedMediaType)
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var reviewResponse *kubeApiAdmission.AdmissionResponse
	ar := kubeApiAdmission.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
	}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		reviewResponse = toAdmissionResponse(fmt.Errorf("could not decode body: %v", err))
	} else {
		reviewResponse = admit(ar.Request)
	}

	response := kubeApiAdmission.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
	}
	if reviewResponse != nil {
		response.Response = reviewResponse
		if ar.Request != nil {
			response.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(response)
	if err != nil {
		reportValidationHTTPError(http.StatusInternalServerError)
		http.Error(w, fmt.Sprintf("could encode response: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(resp); err != nil {
		reportValidationHTTPError(http.StatusInternalServerError)
		http.Error(w, fmt.Sprintf("could write response: %v", err), http.StatusInternalServerError)
	}
}

func (wh *Webhook) serveAdmitPilot(w http.ResponseWriter, r *http.Request) {
	serve(w, r, wh.admitPilot)
}

func (wh *Webhook) serveValidate(w http.ResponseWriter, r *http.Request) {
	serve(w, r, wh.validate)
}

func (wh *Webhook) validate(request *kubeApiAdmission.AdmissionRequest) *kubeApiAdmission.AdmissionResponse {
	switch request.Kind.Kind {
	default:
		return wh.admitPilot(request)
	}
}

func (wh *Webhook) admitPilot(request *kubeApiAdmission.AdmissionRequest) *kubeApiAdmission.AdmissionResponse {
	switch request.Operation {
	case kubeApiAdmission.Create, kubeApiAdmission.Update:
	default:
		scope.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &kubeApiAdmission.AdmissionResponse{Allowed: true}
	}

	var obj crd.IstioKind
	if err := json.Unmarshal(request.Object.Raw, &obj); err != nil {
		scope.Infof("cannot decode configuration: %v", err)
		reportValidationFailed(request, reasonYamlDecodeError)
		return toAdmissionResponse(fmt.Errorf("cannot decode configuration: %v", err))
	}

	gvk := obj.GroupVersionKind()

	// TODO(jasonwzm) remove this when multi-version is supported. v1beta1 shares the same
	// schema as v1lalpha3. Fake conversion and validate against v1alpha3.
	if gvk.Group == "networking.istio.io" && gvk.Version == "v1beta1" {
		gvk.Version = "v1alpha3"
	}
	s, exists := wh.schemas.FindByGroupVersionKind(resource.FromKubernetesGVK(&gvk))
	if !exists {
		scope.Infof("unrecognized type %v", obj.Kind)
		reportValidationFailed(request, reasonUnknownType)
		return toAdmissionResponse(fmt.Errorf("unrecognized type %v", obj.Kind))
	}

	out, err := crd.ConvertObject(s, &obj, wh.domainSuffix)
	if err != nil {
		scope.Infof("error decoding configuration: %v", err)
		reportValidationFailed(request, reasonCRDConversionError)
		return toAdmissionResponse(fmt.Errorf("error decoding configuration: %v", err))
	}

	if err := s.Resource().ValidateProto(out.Name, out.Namespace, out.Spec); err != nil {
		scope.Infof("configuration is invalid: %v", err)
		reportValidationFailed(request, reasonInvalidConfig)
		return toAdmissionResponse(fmt.Errorf("configuration is invalid: %v", err))
	}

	if reason, err := checkFields(request.Object.Raw, request.Kind.Kind, request.Namespace, obj.Name); err != nil {
		reportValidationFailed(request, reason)
		return toAdmissionResponse(err)
	}

	reportValidationPass(request)
	return &kubeApiAdmission.AdmissionResponse{Allowed: true}
}

func checkFields(raw []byte, kind string, namespace string, name string) (string, error) {
	trial := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &trial); err != nil {
		scope.Infof("cannot decode configuration fields: %v", err)
		return reasonYamlDecodeError, fmt.Errorf("cannot decode configuration fields: %v", err)
	}

	for key := range trial {
		if _, ok := validFields[key]; !ok {
			scope.Infof("unknown field %q on %s resource %s/%s",
				key, kind, namespace, name)
			return reasonInvalidConfig, fmt.Errorf("unknown field %q on %s resource %s/%s",
				key, kind, namespace, name)
		}
	}

	return "", nil
}

// validatePort checks that the network port is in range
func validatePort(port int) error {
	if 1 <= port && port <= 65535 {
		return nil
	}
	return fmt.Errorf("port number %d must be in the range 1..65535", port)
}

// Validate tests if the Options has valid params.
func (o Options) Validate() error {
	var errs *multierror.Error
	if err := validatePort(int(o.Port)); err != nil {
		errs = multierror.Append(errs, err)
	}
	return errs.ErrorOrNil()
}
