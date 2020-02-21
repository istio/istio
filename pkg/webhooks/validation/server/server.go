// Copyright 2018 Istio Authors
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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"
	kubeApiAdmission "k8s.io/api/admission/v1beta1"
	kubeApiApps "k8s.io/api/apps/v1beta1"
	kubeApisMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
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

const (
	HTTPSHandlerReadyPath = "/httpsReady"

	watchDebounceDelay = 100 * time.Millisecond
)

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
	Port uint

	// CertFile is the path to the x509 certificate for https.
	CertFile string

	// KeyFile is the path to the x509 private key matching `CertFile`.
	KeyFile string

	// Use an existing mux instead of creating our own.
	Mux *http.ServeMux
}

// String produces a stringified version of the arguments for debugging.
func (o Options) String() string {
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, "DomainSuffix: %s\n", o.DomainSuffix)
	_, _ = fmt.Fprintf(buf, "Port: %d\n", o.Port)
	_, _ = fmt.Fprintf(buf, "CertFile: %s\n", o.CertFile)
	_, _ = fmt.Fprintf(buf, "KeyFile: %s\n", o.KeyFile)

	return buf.String()
}

// DefaultArgs allocates an Options struct initialized with Webhook's default configuration.
func DefaultArgs() Options {
	return Options{
		Port:     9443,
		CertFile: constants.DefaultCertChain,
		KeyFile:  constants.DefaultKey,
	}
}

// Webhook implements the validating admission webhook for validating Istio configuration.
type Webhook struct {
	keyCertWatcher filewatcher.FileWatcher

	mu   sync.RWMutex
	cert *tls.Certificate

	// pilot
	schemas      collection.Schemas
	domainSuffix string

	// mixer
	validator store.BackendValidator

	server   *http.Server
	keyFile  string
	certFile string
}

// Reload the server's cert/key for TLS from file and save it for later use by the https server.
func (wh *Webhook) reloadKeyCert() {
	pair, err := ReloadCertkey(wh.certFile, wh.keyFile)
	if err != nil {
		return
	}

	wh.mu.Lock()
	wh.cert = pair
	wh.mu.Unlock()
}

// Reload the server's cert/key for TLS from file.
func ReloadCertkey(certFile, keyFile string) (*tls.Certificate, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		reportValidationCertKeyUpdateError(err)
		scope.Warnf("Cert/Key reload error: %v", err)
		return nil, err
	}

	reportValidationCertKeyUpdate()
	scope.Info("Cert and Key reloaded")

	var row int
	for _, cert := range pair.Certificate {
		if x509Cert, err := x509.ParseCertificates(cert); err != nil {
			scope.Infof("x509 cert [%v] - ParseCertificates() error: %v\n", row, err)
			row++
		} else {
			for _, c := range x509Cert {
				scope.Infof("x509 cert [%v] - Issuer: %q, Subject: %q, SN: %x, NotBefore: %q, NotAfter: %q\n",
					row, c.Issuer, c.Subject, c.SerialNumber,
					c.NotBefore.Format(time.RFC3339), c.NotAfter.Format(time.RFC3339))
				row++
			}
		}
	}
	return &pair, nil
}

// New creates a new instance of the admission webhook server.
func New(p Options) (*Webhook, error) {
	if p.Mux != nil {
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
	pair, err := ReloadCertkey(p.CertFile, p.KeyFile)
	if err != nil {
		return nil, err
	}

	// Configuration must be updated whenever the caBundle changes. Watch the parent directory of
	// the target files so we can catch symlink updates of k8s secrets.
	keyCertWatcher := filewatcher.NewWatcher()

	for _, file := range []string{p.CertFile, p.KeyFile} {
		if err := keyCertWatcher.Add(file); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}

	wh := &Webhook{
		server: &http.Server{
			Addr: fmt.Sprintf(":%v", p.Port),
		},
		keyFile:        p.KeyFile,
		certFile:       p.CertFile,
		keyCertWatcher: keyCertWatcher,
		cert:           pair,
		schemas:        p.Schemas,
		validator:      p.MixerValidator,
	}

	// mtls disabled because apiserver webhook cert usage is still TBD.
	wh.server.TLSConfig = &tls.Config{GetCertificate: wh.getCert}
	h := http.NewServeMux()
	h.HandleFunc(HTTPSHandlerReadyPath, wh.serveReady)
	h.HandleFunc("/validate", wh.serveValidate)
	// old handlers retained backwards compatibility during upgrades
	h.HandleFunc("/admitpilot", wh.serveAdmitPilot)
	h.HandleFunc("/admitmixer", wh.serveAdmitMixer)
	wh.server.Handler = h

	return wh, nil
}

//Stop the server
func (wh *Webhook) Stop() {
	_ = wh.server.Close()
}

var readyHook = func() {}

// Run implements the webhook server
func (wh *Webhook) Run(stopCh <-chan struct{}) {

	if wh.server == nil {
		// Externally managed
		return
	}
	go func() {
		if err := wh.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			scope.Fatalf("admission webhook ListenAndServeTLS failed: %v", err)
		}
	}()
	defer func() {
		wh.Stop()
	}()

	if readyHook != nil {
		readyHook()
	}

	// use a timer to debounce key/cert updates
	var keyCertTimerC <-chan time.Time

	for {
		select {
		case <-keyCertTimerC:
			keyCertTimerC = nil
			wh.reloadKeyCert()
		case <-wh.keyCertWatcher.Events(wh.keyFile):
			if keyCertTimerC == nil {
				keyCertTimerC = time.After(watchDebounceDelay)
			}
		case <-wh.keyCertWatcher.Events(wh.certFile):
			if keyCertTimerC == nil {
				keyCertTimerC = time.After(watchDebounceDelay)
			}
		case err := <-wh.keyCertWatcher.Errors(wh.keyFile):
			scope.Errorf("configWatcher error: %v", err)
		case err := <-wh.keyCertWatcher.Errors(wh.certFile):
			scope.Errorf("configWatcher error: %v", err)
		case <-stopCh:
			return
		}
	}
}

func (wh *Webhook) getCert(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	wh.mu.Lock()
	defer wh.mu.Unlock()
	return wh.cert, nil
}

func toAdmissionResponse(err error) *kubeApiAdmission.AdmissionResponse {
	return &kubeApiAdmission.AdmissionResponse{Result: &kubeApisMeta.Status{Message: err.Error()}}
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
	ar := kubeApiAdmission.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		reviewResponse = toAdmissionResponse(fmt.Errorf("could not decode body: %v", err))
	} else {
		reviewResponse = admit(ar.Request)
	}

	response := kubeApiAdmission.AdmissionReview{}
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

func (wh *Webhook) serveReady(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
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

func (wh *Webhook) validate(request *kubeApiAdmission.AdmissionRequest) *kubeApiAdmission.AdmissionResponse {
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

func (wh *Webhook) admitPilot(request *kubeApiAdmission.AdmissionRequest) *kubeApiAdmission.AdmissionResponse {
	switch request.Operation {
	case kubeApiAdmission.Create, kubeApiAdmission.Update:
	default:
		scope.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &kubeApiAdmission.AdmissionResponse{Allowed: true}
	}

	var obj crd.IstioKind
	if err := yaml.Unmarshal(request.Object.Raw, &obj); err != nil {
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

func (wh *Webhook) admitMixer(request *kubeApiAdmission.AdmissionRequest) *kubeApiAdmission.AdmissionResponse {
	ev := &store.BackendEvent{
		Key: store.Key{
			Namespace: request.Namespace,
			Kind:      request.Kind.Kind,
		},
	}
	switch request.Operation {
	case kubeApiAdmission.Create, kubeApiAdmission.Update:
		ev.Type = store.Update
		var obj crd.IstioKind
		if err := yaml.Unmarshal(request.Object.Raw, &obj); err != nil {
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

	case kubeApiAdmission.Delete:
		if request.Name == "" {
			reportValidationFailed(request, reasonUnknownType)
			return toAdmissionResponse(fmt.Errorf("illformed request: name not found on delete request"))
		}
		ev.Type = store.Delete
		ev.Key.Name = request.Name
	default:
		scope.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &kubeApiAdmission.AdmissionResponse{Allowed: true}
	}

	// webhook skips deletions
	if ev.Type == store.Update {
		if err := wh.validator.Validate(ev); err != nil {
			reportValidationFailed(request, reasonInvalidConfig)
			return toAdmissionResponse(err)
		}
	}

	reportValidationPass(request)
	return &kubeApiAdmission.AdmissionResponse{Allowed: true}
}

func checkFields(raw []byte, kind string, namespace string, name string) (string, error) {
	trial := make(map[string]json.RawMessage)
	if err := yaml.Unmarshal(raw, &trial); err != nil {
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
	if len(o.CertFile) == 0 {
		errs = multierror.Append(errs, errors.New("cert file not specified"))
	}
	if len(o.KeyFile) == 0 {
		errs = multierror.Append(errs, errors.New("key file not specified"))
	}
	if err := validatePort(int(o.Port)); err != nil {
		errs = multierror.Append(errs, err)
	}
	return errs.ErrorOrNil()
}
