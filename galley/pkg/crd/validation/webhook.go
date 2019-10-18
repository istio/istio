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

package validation

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/howeyc/fsnotify"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	mixerCrd "istio.io/istio/mixer/pkg/config/crd"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema"
)

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
	_ = v1beta1.AddToScheme(runtimeScheme)
}

const (
	watchDebounceDelay             = 100 * time.Millisecond
	retryUpdateAfterFailureTimeout = time.Second

	httpsHandlerReadyPath = "/ready"
)

// WebhookParameters contains the configuration for the Istio Pilot validation
// admission controller.
type WebhookParameters struct {
	// MixerValidator implements the backend validator functions for mixer configuration.
	MixerValidator store.BackendValidator

	// PilotDescriptor provides a description of all pilot configuration resources.
	PilotDescriptor schema.Set

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

	// WebhookConfigFile is the path to the validatingwebhookconfiguration
	// file that should be used for self-registration.
	WebhookConfigFile string

	// CACertFile is the path to the x509 CA bundle file.
	CACertFile string

	// DeploymentAndServiceNamespace is the namespace in which the validation deployment and service resides.
	DeploymentAndServiceNamespace string

	// Name of the k8s validatingwebhookconfiguration
	WebhookName string

	// DeploymentName is the name of the validation deployment. This, along with
	// DeploymentAndServiceNamespace, is used to set the ownerReference in the
	// validatingwebhookconfiguration. This enables k8s to clean-up the cluster-scoped
	// validatingwebhookconfiguration when the deployment is deleted.
	DeploymentName string

	// ServiceName is the name of the k8s service of the validation webhook. This is
	// used to verify endpoint readiness before registering the validatingwebhookconfiguration.
	ServiceName string

	Clientset clientset.Interface

	// Enable galley validation mode
	EnableValidation bool

	// Enable reconcile validatingwebhookconfiguration
	EnableReconcileWebhookConfiguration bool
}

type createInformerEndpointSource func(cl clientset.Interface, namespace, name string) cache.ListerWatcher

var (
	defaultCreateInformerEndpointSource = func(cl clientset.Interface, namespace, name string) cache.ListerWatcher {
		return cache.NewListWatchFromClient(
			cl.CoreV1().RESTClient(),
			"endpoints",
			namespace,
			fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", name)))
	}
)

// String produces a stringified version of the arguments for debugging.
func (p *WebhookParameters) String() string {
	buf := &bytes.Buffer{}

	fmt.Fprintf(buf, "DomainSuffix: %s\n", p.DomainSuffix)
	fmt.Fprintf(buf, "Port: %d\n", p.Port)
	fmt.Fprintf(buf, "CertFile: %s\n", p.CertFile)
	fmt.Fprintf(buf, "KeyFile: %s\n", p.KeyFile)
	fmt.Fprintf(buf, "WebhookConfigFile: %s\n", p.WebhookConfigFile)
	fmt.Fprintf(buf, "CACertFile: %s\n", p.CACertFile)
	fmt.Fprintf(buf, "DeploymentAndServiceNamespace: %s\n", p.DeploymentAndServiceNamespace)
	fmt.Fprintf(buf, "WebhookName: %s\n", p.WebhookName)
	fmt.Fprintf(buf, "DeploymentName: %s\n", p.DeploymentName)
	fmt.Fprintf(buf, "ServiceName: %s\n", p.ServiceName)
	fmt.Fprintf(buf, "EnableValidation: %v\n", p.EnableValidation)
	fmt.Fprintf(buf, "EnableReconcileWebhookConfiguration: %v\n", p.EnableReconcileWebhookConfiguration)

	return buf.String()
}

// DefaultArgs allocates an WebhookParameters struct initialized with Webhook's default configuration.
func DefaultArgs() *WebhookParameters {
	return &WebhookParameters{
		Port:                                9443,
		CertFile:                            constants.DefaultCertChain,
		KeyFile:                             constants.DefaultKey,
		CACertFile:                          constants.DefaultRootCert,
		DeploymentAndServiceNamespace:       "istio-system",
		DeploymentName:                      "istio-galley",
		ServiceName:                         "istio-galley",
		WebhookName:                         "istio-galley",
		EnableValidation:                    true,
		EnableReconcileWebhookConfiguration: true,
	}
}

// Webhook implements the validating admission webhook for validating Istio configuration.
type Webhook struct {
	keyCertWatcher *fsnotify.Watcher

	mu   sync.RWMutex
	cert *tls.Certificate

	// pilot
	descriptor   schema.Set
	domainSuffix string

	// mixer
	validator store.BackendValidator

	server                        *http.Server
	clientset                     clientset.Interface
	deploymentAndServiceNamespace string
	deploymentName                string
	serviceName                   string
	webhookName                   string
	keyFile                       string
	certFile                      string

	// test hook for informers
	createInformerEndpointSource createInformerEndpointSource
}

// Reload the server's cert/key for TLS from file and save it for later use by the https server.
func (wh *Webhook) reloadKeyCert() {
	pair, err := reloadKeyCert(wh.certFile, wh.keyFile)
	if err != nil {
		return
	}

	wh.mu.Lock()
	wh.cert = pair
	wh.mu.Unlock()
}

// Reload the server's cert/key for TLS from file.
func reloadKeyCert(certFile, keyFile string) (*tls.Certificate, error) {
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

// NewWebhook creates a new instance of the admission webhook controller.
func NewWebhook(p WebhookParameters) (*Webhook, error) {
	pair, err := reloadKeyCert(p.CertFile, p.KeyFile)
	if err != nil {
		return nil, err
	}

	// Configuration must be updated whenever the caBundle changes. Watch the parent directory of
	// the target files so we can catch symlink updates of k8s secrets.
	keyCertWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	for _, file := range []string{p.CertFile, p.KeyFile} {
		watchDir, _ := filepath.Split(file)
		if err := keyCertWatcher.Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}

	wh := &Webhook{
		server: &http.Server{
			Addr: fmt.Sprintf(":%v", p.Port),
		},
		keyFile:                       p.KeyFile,
		certFile:                      p.CertFile,
		keyCertWatcher:                keyCertWatcher,
		cert:                          pair,
		descriptor:                    p.PilotDescriptor,
		validator:                     p.MixerValidator,
		clientset:                     p.Clientset,
		deploymentName:                p.DeploymentName,
		serviceName:                   p.ServiceName,
		webhookName:                   p.WebhookName,
		deploymentAndServiceNamespace: p.DeploymentAndServiceNamespace,
		createInformerEndpointSource:  defaultCreateInformerEndpointSource,
	}

	// mtls disabled because apiserver webhook cert usage is still TBD.
	wh.server.TLSConfig = &tls.Config{GetCertificate: wh.getCert}
	h := http.NewServeMux()
	h.HandleFunc("/admitpilot", wh.serveAdmitPilot)
	h.HandleFunc("/admitmixer", wh.serveAdmitMixer)
	h.HandleFunc(httpsHandlerReadyPath, wh.serveReady)
	wh.server.Handler = h

	return wh, nil
}

//Stop the server
func (wh *Webhook) Stop() {
	wh.server.Close() // nolint: errcheck
}

// Run implements the webhook server
func (wh *Webhook) Run(ready chan<- struct{}, stopCh <-chan struct{}) {
	go func() {
		if err := wh.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			scope.Fatalf("admission webhook ListenAndServeTLS failed: %v", err)
		}
	}()
	defer func() {
		wh.Stop()
	}()

	// During initial Istio installation its possible for custom
	// resources to be created concurrently with galley startup. This
	// can lead to validation failures with "no endpoints available"
	// if the webhook is registered before the endpoint is visible to
	// the rest of the system. Minimize this problem by waiting for the
	// galley endpoint to be available at least once before
	// self-registering. Subsequent Istio upgrades rely on deployment
	// rolling updates to set maxUnavailable to zero.
	if shutdown := wh.waitForEndpointReady(stopCh); shutdown {
		return
	}

	ready <- struct{}{}

	// use a timer to debounce key/cert updates
	var keyCertTimerC <-chan time.Time

	for {
		select {
		case <-keyCertTimerC:
			keyCertTimerC = nil
			wh.reloadKeyCert()
		case event, more := <-wh.keyCertWatcher.Event:
			if more && (event.IsModify() || event.IsCreate()) && keyCertTimerC == nil {
				keyCertTimerC = time.After(watchDebounceDelay)
			}
		case err := <-wh.keyCertWatcher.Error:
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

func toAdmissionResponse(err error) *admissionv1beta1.AdmissionResponse {
	return &admissionv1beta1.AdmissionResponse{Result: &v1.Status{Message: err.Error()}}
}

type admitFunc func(*admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse

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

	var reviewResponse *admissionv1beta1.AdmissionResponse
	ar := admissionv1beta1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		reviewResponse = toAdmissionResponse(fmt.Errorf("could not decode body: %v", err))
	} else {
		reviewResponse = admit(ar.Request)
	}

	response := admissionv1beta1.AdmissionReview{}
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

func (wh *Webhook) serveReady(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (wh *Webhook) serveAdmitPilot(w http.ResponseWriter, r *http.Request) {
	serve(w, r, wh.admitPilot)
}

func (wh *Webhook) serveAdmitMixer(w http.ResponseWriter, r *http.Request) {
	serve(w, r, wh.admitMixer)
}

func (wh *Webhook) admitPilot(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	switch request.Operation {
	case admissionv1beta1.Create, admissionv1beta1.Update:
	default:
		scope.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &admissionv1beta1.AdmissionResponse{Allowed: true}
	}

	var obj crd.IstioKind
	if err := yaml.Unmarshal(request.Object.Raw, &obj); err != nil {
		scope.Infof("cannot decode configuration: %v", err)
		reportValidationFailed(request, reasonYamlDecodeError)
		return toAdmissionResponse(fmt.Errorf("cannot decode configuration: %v", err))
	}

	s, exists := wh.descriptor.GetByType(crd.CamelCaseToKebabCase(obj.Kind))
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

	if err := s.Validate(out.Name, out.Namespace, out.Spec); err != nil {
		scope.Infof("configuration is invalid: %v", err)
		reportValidationFailed(request, reasonInvalidConfig)
		return toAdmissionResponse(fmt.Errorf("configuration is invalid: %v", err))
	}

	if reason, err := checkFields(request.Object.Raw, request.Kind.Kind, request.Namespace, obj.Name); err != nil {
		reportValidationFailed(request, reason)
		return toAdmissionResponse(err)
	}

	reportValidationPass(request)
	return &admissionv1beta1.AdmissionResponse{Allowed: true}
}

func (wh *Webhook) admitMixer(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	ev := &store.BackendEvent{
		Key: store.Key{
			Namespace: request.Namespace,
			Kind:      request.Kind.Kind,
		},
	}
	switch request.Operation {
	case admissionv1beta1.Create, admissionv1beta1.Update:
		ev.Type = store.Update
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(request.Object.Raw, &obj); err != nil {
			reportValidationFailed(request, reasonYamlDecodeError)
			return toAdmissionResponse(fmt.Errorf("cannot decode configuration: %v", err))
		}

		ev.Value = mixerCrd.ToBackEndResource(&obj)
		ev.Key.Name = ev.Value.Metadata.Name

		if reason, err := checkFields(request.Object.Raw, request.Kind.Kind, request.Namespace, ev.Key.Name); err != nil {
			reportValidationFailed(request, reason)
			return toAdmissionResponse(err)
		}

	case admissionv1beta1.Delete:
		if request.Name == "" {
			reportValidationFailed(request, reasonUnknownType)
			return toAdmissionResponse(fmt.Errorf("illformed request: name not found on delete request"))
		}
		ev.Type = store.Delete
		ev.Key.Name = request.Name
	default:
		scope.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &admissionv1beta1.AdmissionResponse{Allowed: true}
	}

	// webhook skips deletions
	if ev.Type == store.Update {
		if err := wh.validator.Validate(ev); err != nil {
			reportValidationFailed(request, reasonInvalidConfig)
			return toAdmissionResponse(err)
		}
	}

	reportValidationPass(request)
	return &admissionv1beta1.AdmissionResponse{Allowed: true}
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
