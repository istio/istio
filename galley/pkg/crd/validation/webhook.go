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
	"crypto/tls"
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
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	clientset "k8s.io/client-go/kubernetes"

	mixerCrd "istio.io/istio/mixer/pkg/config/crd"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

const (
	watchDebounceDelay = 100 * time.Millisecond
)

// WebhookParameters contains the configuration for the Istio Pilot validation
// admission controller.
type WebhookParameters struct {
	// MixerValidator implements the backend validator functions for mixer configuration.
	MixerValidator store.BackendValidator

	// PilotDescriptor provides a description of all pilot configuration resources.
	PilotDescriptor model.ConfigDescriptor

	// DomainSuffix is the DNS domain suffix for Pilot CRD resources,
	// e.g. cluster.local.
	DomainSuffix string

	// Port where the webhook is served. Per k8s admission
	// registration requirements this should be 443 unless there is
	// only a single port for the service.
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

	// DeploymentNamespace is the namespace in which the validation deployment resides.
	DeploymentNamespace string

	// DeploymentName is the name of the validation deployment. This, along with
	// DeploymentNamespace, is used to set the ownerReference in the
	// validatingwebhookconfiguration. This enables k8s to clean-up the cluster-scoped
	// validatingwebhookconfiguration when the deployment is deleted.
	DeploymentName string

	Clientset clientset.Interface

	// Enable galley validation mode
	EnableValidation bool
}

// Webhook implements the validating admission webhook for validating Istio configuration.
type Webhook struct {
	mu   sync.RWMutex
	cert *tls.Certificate

	// pilot
	descriptor   model.ConfigDescriptor
	domainSuffix string

	// mixer
	validator store.BackendValidator

	server               *http.Server
	keyCertWatcher       *fsnotify.Watcher
	configWatcher        *fsnotify.Watcher
	certFile             string
	keyFile              string
	caFile               string
	webhookConfigFile    string
	clientset            clientset.Interface
	deploymentNamespace  string
	deploymentName       string
	ownerRefs            []v1.OwnerReference
	webhookConfiguration *v1beta1.ValidatingWebhookConfiguration
	endpointReadyOnce    bool
}

// NewWebhook creates a new instance of the admission webhook controller.
func NewWebhook(p WebhookParameters) (*Webhook, error) {
	pair, err := tls.LoadX509KeyPair(p.CertFile, p.KeyFile)
	if err != nil {
		return nil, err
	}
	certKeyWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// watch the parent directory of the target files so we can catch
	// symlink updates of k8s secrets
	for _, file := range []string{p.CertFile, p.KeyFile, p.CACertFile, p.WebhookConfigFile} {
		watchDir, _ := filepath.Split(file)
		if err := certKeyWatcher.Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}

	// configuration must be updated whenever the caBundle changes.
	// NOTE: Use a separate watcher to differentiate config/ca from cert/key updates. This is
	// useful to avoid unnecessary updates and, more importantly, makes its easier to more
	// accurately capture logs/metrics when files change.
	configWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	for _, file := range []string{p.CACertFile, p.WebhookConfigFile} {
		watchDir, _ := filepath.Split(file)
		if err := configWatcher.Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}

	wh := &Webhook{
		server: &http.Server{
			Addr: fmt.Sprintf(":%v", p.Port),
		},
		keyCertWatcher:      certKeyWatcher,
		configWatcher:       configWatcher,
		certFile:            p.CertFile,
		keyFile:             p.KeyFile,
		cert:                &pair,
		descriptor:          p.PilotDescriptor,
		validator:           p.MixerValidator,
		caFile:              p.CACertFile,
		webhookConfigFile:   p.WebhookConfigFile,
		clientset:           p.Clientset,
		deploymentName:      p.DeploymentName,
		deploymentNamespace: p.DeploymentNamespace,
	}

	if galleyDeployment, err := wh.clientset.ExtensionsV1beta1().Deployments(wh.deploymentNamespace).Get(wh.deploymentName, v1.GetOptions{}); err != nil { // nolint: lll
		log.Warnf("Could not find %s/%s deployment to set ownerRef. The validatingwebhookconfiguration must be deleted manually",
			wh.deploymentNamespace, wh.deploymentName)
	} else {
		wh.ownerRefs = []v1.OwnerReference{
			*v1.NewControllerRef(
				galleyDeployment,
				extensionsv1beta1.SchemeGroupVersion.WithKind("Deployment"),
			),
		}
	}

	// mtls disabled because apiserver webhook cert usage is still TBD.
	wh.server.TLSConfig = &tls.Config{GetCertificate: wh.getCert}
	h := http.NewServeMux()
	h.HandleFunc("/admitpilot", wh.serveAdmitPilot)
	h.HandleFunc("/admitmixer", wh.serveAdmitMixer)
	wh.server.Handler = h

	return wh, nil
}

func (wh *Webhook) stop() {
	wh.keyCertWatcher.Close() // nolint: errcheck
	wh.configWatcher.Close()  // nolint: errcheck
	wh.server.Close()         // nolint: errcheck
}

// Run implements the webhook server
func (wh *Webhook) Run(stop <-chan struct{}) {
	go func() {
		if err := wh.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("admission webhook ListenAndServeTLS failed: %v", err)
		}
	}()
	defer wh.stop()

	// use a timer to debounce file updates
	var keyCertTimerC <-chan time.Time
	var configTimerC <-chan time.Time
	var reconcileTickerC <-chan time.Time

	if wh.webhookConfigFile != "" {
		log.Info("server-side configuration validation enabled")
		reconcileTickerC = time.NewTicker(time.Second).C
	} else {
		log.Info("server-side configuration validation disabled. Enable with --webhook-config-file")
	}

	for {
		select {
		case <-keyCertTimerC:
			keyCertTimerC = nil
			wh.reloadKeyCert()
		case <-configTimerC:
			configTimerC = nil
			if err := wh.rebuildWebhookConfig(); err == nil {
				wh.createOrUpdateWebhookConfig()
			}
		case <-reconcileTickerC:
			if wh.webhookConfiguration == nil {
				if err := wh.rebuildWebhookConfig(); err == nil {
					wh.createOrUpdateWebhookConfig()
				}
			} else {
				wh.createOrUpdateWebhookConfig()
			}
		case event, more := <-wh.keyCertWatcher.Event:
			if more && (event.IsModify() || event.IsCreate()) && keyCertTimerC == nil {
				keyCertTimerC = time.After(watchDebounceDelay)
			}
		case event, more := <-wh.configWatcher.Event:
			if more && (event.IsModify() || event.IsCreate()) && configTimerC == nil {
				configTimerC = time.After(watchDebounceDelay)
			}
		case err := <-wh.keyCertWatcher.Error:
			log.Errorf("keyCertWatcher error: %v", err)
		case err := <-wh.configWatcher.Error:
			log.Errorf("configWatcher error: %v", err)
		case <-stop:
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
	return &admissionv1beta1.AdmissionResponse{Result: &metav1.Status{Message: err.Error()}}
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
		log.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &admissionv1beta1.AdmissionResponse{Allowed: true}
	}

	var obj crd.IstioKind
	if err := yaml.Unmarshal(request.Object.Raw, &obj); err != nil {
		reportValidationFailed(request, reasonYamlDecodeError)
		return toAdmissionResponse(fmt.Errorf("cannot decode configuration: %v", err))
	}

	schema, exists := wh.descriptor.GetByType(crd.CamelCaseToKabobCase(obj.Kind))
	if !exists {
		reportValidationFailed(request, reasonUnknownType)
		return toAdmissionResponse(fmt.Errorf("unrecognized type %v", obj.Kind))
	}

	out, err := crd.ConvertObject(schema, &obj, wh.domainSuffix)
	if err != nil {
		reportValidationFailed(request, reasonCRDConversionError)
		return toAdmissionResponse(fmt.Errorf("error decoding configuration: %v", err))
	}

	if err := schema.Validate(out.Name, out.Namespace, out.Spec); err != nil {
		reportValidationFailed(request, reasonInvalidConfig)
		return toAdmissionResponse(fmt.Errorf("configuration is invalid: %v", err))
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
	case admissionv1beta1.Delete:
		if request.Name == "" {
			reportValidationFailed(request, reasonUnknownType)
			return toAdmissionResponse(fmt.Errorf("illformed request: name not found on delete request"))
		}
		ev.Type = store.Delete
		ev.Key.Name = request.Name
	default:
		log.Warnf("Unsupported webhook operation %v", request.Operation)
		reportValidationFailed(request, reasonUnsupportedOperation)
		return &admissionv1beta1.AdmissionResponse{Allowed: true}
	}

	if err := wh.validator.Validate(ev); err != nil {
		reportValidationFailed(request, reasonInvalidConfig)
		return toAdmissionResponse(err)
	}

	reportValidationPass(request)
	return &admissionv1beta1.AdmissionResponse{Allowed: true}
}
