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

	multierror "github.com/hashicorp/go-multierror"
	kubeApiAdmissionv1 "k8s.io/api/admission/v1"
	kubeApiAdmissionv1beta1 "k8s.io/api/admission/v1beta1"
	kubeApiApps "k8s.io/api/apps/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
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
	_ = kubeApiAdmissionv1.AddToScheme(runtimeScheme)
	_ = kubeApiAdmissionv1beta1.AddToScheme(runtimeScheme)
}

// Options contains the configuration for the Istio Pilot validation
// admission controller.
type Options struct {
	// MixerValidator implements the backend validator functions for mixer configuration.
	MixerValidator store.BackendValidator

	// Schemas provides a description of all configuration resources excluding mixer types.
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

	// mixer
	validator store.BackendValidator
}

// New creates a new instance of the admission webhook server.
func New(p Options) (*Webhook, error) {
	if p.Mux == nil {
		scope.Error("mux not set correctly")
		return nil, errors.New("expected mux to be passed, but was not passed")
	}
	wh := &Webhook{
		schemas:   p.Schemas,
		validator: p.MixerValidator,
	}

	p.Mux.HandleFunc("/validate", wh.serveValidate)
	// old handlers retained backwards compatibility during upgrades
	p.Mux.HandleFunc("/admitpilot", wh.serveAdmitPilot)
	p.Mux.HandleFunc("/admitmixer", wh.serveAdmitMixer)

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

func toAdmissionResponse(err error) *kube.AdmissionResponse {
	return &kube.AdmissionResponse{Result: &metav1.Status{Message: err.Error()}}
}

type admitFunc func(*kube.AdmissionRequest) *kube.AdmissionResponse

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

	var reviewResponse *kube.AdmissionResponse
	var obj runtime.Object
	var ar *kube.AdmissionReview
	if out, _, err := deserializer.Decode(body, nil, obj); err != nil {
		reviewResponse = toAdmissionResponse(fmt.Errorf("could not decode body: %v", err))
	} else {
		ar, err = kube.AdmissionReviewKubeToAdapter(out)
		if err != nil {
			reviewResponse = toAdmissionResponse(fmt.Errorf("could not decode object: %v", err))
		} else {
			reviewResponse = admit(ar.Request)
		}
	}

	response := kube.AdmissionReview{}
	response.Response = reviewResponse
	var responseKube runtime.Object
	var apiVersion string
	if ar != nil {
		apiVersion = ar.APIVersion
		response.TypeMeta = ar.TypeMeta
		if response.Response != nil {
			if ar.Request != nil {
				response.Response.UID = ar.Request.UID
			}
		}
	}
	responseKube = kube.AdmissionReviewAdapterToKube(&response, apiVersion)
	resp, err := json.Marshal(responseKube)
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

func (wh *Webhook) serveAdmitMixer(w http.ResponseWriter, r *http.Request) {
	serve(w, r, wh.admitMixer)
}

func (wh *Webhook) serveValidate(w http.ResponseWriter, r *http.Request) {
	serve(w, r, wh.validate)
}

func (wh *Webhook) validate(request *kube.AdmissionRequest) *kube.AdmissionResponse {
	switch request.Kind.Kind {
	case collections.IstioPolicyV1Beta1Rules.Resource().Kind(),
		collections.IstioPolicyV1Beta1Attributemanifests.Resource().Kind(),
		collections.IstioConfigV1Alpha2Adapters.Resource().Kind(),
		collections.IstioPolicyV1Beta1Handlers.Resource().Kind(),
		collections.IstioPolicyV1Beta1Instances.Resource().Kind(),
		collections.IstioConfigV1Alpha2Templates.Resource().Kind():
		return wh.admitMixer(request)
	default:
		return wh.admitPilot(request)
	}
}

func (wh *Webhook) admitPilot(request *kube.AdmissionRequest) *kube.AdmissionResponse {
	switch request.Operation {
	case kube.Create, kube.Update:
	default:
		scope.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &kube.AdmissionResponse{Allowed: true}
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
	return &kube.AdmissionResponse{Allowed: true}
}

func (wh *Webhook) admitMixer(request *kube.AdmissionRequest) *kube.AdmissionResponse {
	ev := &store.BackendEvent{
		Key: store.Key{
			Namespace: request.Namespace,
			Kind:      request.Kind.Kind,
		},
	}
	switch request.Operation {
	case kube.Create, kube.Update:
		ev.Type = store.Update
		var obj crd.IstioKind
		if err := json.Unmarshal(request.Object.Raw, &obj); err != nil {
			reportValidationFailed(request, reasonYamlDecodeError)
			return toAdmissionResponse(fmt.Errorf("cannot decode configuration: %v", err))
		}

		ev.Value = &store.BackEndResource{
			Metadata: store.ResourceMeta{
				Name:        obj.Name,
				Namespace:   obj.Namespace,
				Labels:      obj.Labels,
				Annotations: obj.Annotations,
				Revision:    obj.ResourceVersion,
			},
			Spec: obj.Spec,
		}
		ev.Key.Name = ev.Value.Metadata.Name

		if reason, err := checkFields(request.Object.Raw, request.Kind.Kind, request.Namespace, ev.Key.Name); err != nil {
			reportValidationFailed(request, reason)
			return toAdmissionResponse(err)
		}

	case kube.Delete:
		if request.Name == "" {
			reportValidationFailed(request, reasonUnknownType)
			return toAdmissionResponse(fmt.Errorf("illformed request: name not found on delete request"))
		}
		ev.Type = store.Delete
		ev.Key.Name = request.Name
	default:
		scope.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &kube.AdmissionResponse{Allowed: true}
	}

	// webhook skips deletions
	if ev.Type == store.Update {
		if err := wh.validator.Validate(ev); err != nil {
			reportValidationFailed(request, reasonInvalidConfig)
			return toAdmissionResponse(err)
		}
	}

	reportValidationPass(request)
	return &kube.AdmissionResponse{Allowed: true}
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
