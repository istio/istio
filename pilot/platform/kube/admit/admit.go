// Copyright 2017 Istio Authors
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

package admit

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"k8s.io/api/admission/v1alpha1"
	admissionregistrationv1alpha1 "k8s.io/api/admissionregistration/v1alpha1"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/client-go/kubernetes"
	admissionClient "k8s.io/client-go/kubernetes/typed/admissionregistration/v1alpha1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/adapter/config/crd"
	"istio.io/istio/pilot/model"
)

const (
	secretServerKey  = "server-key.pem"
	secretServerCert = "server-cert.pem"
	secretCACert     = "ca-cert.pem"
)

// ControllerOptions contains the configuration for the Istio Pilot validation
// admission controller.
type ControllerOptions struct {
	// Descriptor defines the list of supported configuration model
	// types for Pilot.
	Descriptor model.ConfigDescriptor

	// ExternalAdmissionWebhookName is the name of the
	// ExternalAdmissionHook which describes he external admission
	// webhook and resources and operations it applies to.
	ExternalAdmissionWebhookName string

	// ServiceName is the service name of the webhook.
	ServiceName string

	// ServiceNamespace is the namespace of the webhook service.
	ServiceNamespace string

	// ValidateNamespaces is a list of names to validate. Any
	// namespace not in this list is unconditionally validated as
	// good. This is useful when multiple validators are running in
	// the same cluster managing different sets of namespaces
	// (e.g. shared test clusters).
	ValidateNamespaces []string

	// // CAbundle is the PEM encoded CA bundle which will be used to
	// // validate webhook's service certificate.
	// CABundle []byte

	// DomainSuffix is the DNS domain suffix for Istio CRD resources,
	// e.g. cluster.local.
	DomainSuffix string

	// SecretName is the name of k8s secret that contains the webhook
	// server key/cert and corresponding CA cert that signed them. The
	// server key/cert are used to serve the webhook and the CA cert
	// is provided to k8s apiserver during admission controller
	// registration.
	SecretName string

	// Port where the webhook is served. Per k8s admission
	// registration requirements this should be 443 unless there is
	// only a single port for the service.
	Port int

	// RegistrationDelay controls how long admission registration
	// occurs after the webhook is started. This is used to avoid
	// potential races where registration completes and k8s apiserver
	// invokes the webhook before the HTTP server is started.
	RegistrationDelay time.Duration
}

// AdmissionController implements the external admission webhook for validation of
// pilot configuration.
type AdmissionController struct {
	client  kubernetes.Interface
	options ControllerOptions
}

// GetAPIServerExtensionCACert gets the Kubernetes aggregate apiserver
// client CA cert used by the "GenericAdmissionWebhook" plugin
// admission controller.
//
// NOTE: this certificate is provided kubernetes. We do not control
// its name or location.
func getAPIServerExtensionCACert(cl kubernetes.Interface) ([]byte, error) {
	const name = "extension-apiserver-authentication"
	c, err := cl.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pem, ok := c.Data["requestheader-client-ca-file"]
	if !ok {
		return nil, fmt.Errorf("cannot find ca.crt in %v: ConfigMap.Data is %#v", name, c.Data)
	}
	return []byte(pem), nil
}

// MakeTLSConfig makes a TLS configuration suitable for use with the
// GenericAdmissionWebhook.
func makeTLSConfig(serverCert, serverKey, caCert []byte) (*tls.Config, error) {
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cert, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

func getKeyCertsFromSecret(client kubernetes.Interface, name, namespace string) (serverKey, serverCert, caCert []byte, err error) { // nolint: lll
	listWatch := cache.NewListWatchFromClient(client.CoreV1().RESTClient(),
		"secrets", namespace, fields.OneTermEqualSelector("metadata.name", name))
	var secret *v1.Secret
	stop := make(chan struct{})
	_, controller := cache.NewInformer(listWatch, &v1.Secret{}, 30*time.Second,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if secret == nil {
					secret = obj.(*v1.Secret)
					close(stop)
				}
			},
		},
	)
	controller.Run(stop)

	var ok bool
	if serverKey, ok = secret.Data[secretServerKey]; !ok {
		return nil, nil, nil, errors.New("server key missing")
	}
	if serverCert, ok = secret.Data[secretServerCert]; !ok {
		return nil, nil, nil, errors.New("server cert missing")
	}
	if caCert, ok = secret.Data[secretCACert]; !ok {
		return nil, nil, nil, errors.New("ca cert missing")
	}
	return serverKey, serverCert, caCert, nil
}

// NewController creates a new instance of the admission webhook controller.
func NewController(client kubernetes.Interface, options ControllerOptions) (*AdmissionController, error) {
	return &AdmissionController{
		client:  client,
		options: options,
	}, nil
}

func setup(client kubernetes.Interface, options *ControllerOptions) (*tls.Config, []byte, error) {
	apiServerCACert, err := getAPIServerExtensionCACert(client)
	if err != nil {
		return nil, nil, err
	}
	serverKey, serverCert, caCert, err := getKeyCertsFromSecret(
		client, options.SecretName, options.ServiceNamespace)
	if err != nil {
		return nil, nil, err
	}
	tlsConfig, err := makeTLSConfig(serverCert, serverKey, apiServerCACert)
	if err != nil {
		return nil, nil, err
	}
	return tlsConfig, caCert, nil
}

// Run implements the admission controller run loop.
func (ac *AdmissionController) Run(stop <-chan struct{}) {
	// TODO(github.com/kubernetes/kubernetes/issues/49987) -
	// Temporarily defer cert generation and registration to the run
	// loop where it won't block other controllers. Ideally this
	// should be performed synchronously as part of NewController()
	// but cert generation (GetKeyCertsFromSecret) and webhooks in
	// general may be optional (default off) until
	// https://github.com/kubernetes/kubernetes/issues/49987 is fixed
	// in GKE 1.8.
	tlsConfig, caCert, err := setup(ac.client, &ac.options)
	if err != nil {
		glog.Errorf(err.Error())
		return
	}

	server := &http.Server{
		Handler:   ac,
		Addr:      fmt.Sprintf(":%v", ac.options.Port),
		TLSConfig: tlsConfig,
	}

	glog.Info("Found certificates for validation admission webhook. Delaying registration for %v",
		ac.options.RegistrationDelay)

	select {
	case <-time.After(ac.options.RegistrationDelay):
		cl := ac.client.AdmissionregistrationV1alpha1().ExternalAdmissionHookConfigurations()
		if err := ac.register(cl, caCert); err != nil {
			glog.Errorf("Failed to register admission webhook: %v", err)
			return
		}
		defer func() {
			if err := ac.unregister(cl); err != nil {
				glog.Errorf("Failed to unregister admission webhook: %v", err)
			}
		}()
		glog.Info("Finished validation admission webhook registration")
	case <-stop:
		return
	}

	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil {
			glog.Errorf("ListenAndServeTLS for admission webhook returned error: %v", err)
		}
	}()
	<-stop
	server.Close() // nolint: errcheck
}

// Unregister unregisters the external admission webhook
func (ac *AdmissionController) unregister(client admissionClient.ExternalAdmissionHookConfigurationInterface) error {
	return client.Delete(ac.options.ExternalAdmissionWebhookName, nil)
}

// Register registers the external admission webhook for pilot
// configuration types.
func (ac *AdmissionController) register(client admissionClient.ExternalAdmissionHookConfigurationInterface, caCert []byte) error { // nolint: lll
	var resources []string
	for _, schema := range ac.options.Descriptor {
		resources = append(resources, crd.ResourceName(schema.Plural))
	}

	webhook := &admissionregistrationv1alpha1.ExternalAdmissionHookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: ac.options.ExternalAdmissionWebhookName,
		},
		ExternalAdmissionHooks: []admissionregistrationv1alpha1.ExternalAdmissionHook{
			{
				Name: ac.options.ExternalAdmissionWebhookName,
				Rules: []admissionregistrationv1alpha1.RuleWithOperations{{
					Operations: []admissionregistrationv1alpha1.OperationType{
						admissionregistrationv1alpha1.Create,
						admissionregistrationv1alpha1.Update,
					},
					Rule: admissionregistrationv1alpha1.Rule{
						APIGroups:   []string{model.IstioAPIGroup},
						APIVersions: []string{model.IstioAPIVersion},
						Resources:   resources,
					},
				}},
				ClientConfig: admissionregistrationv1alpha1.AdmissionHookClientConfig{
					Service: admissionregistrationv1alpha1.ServiceReference{
						Namespace: ac.options.ServiceNamespace,
						Name:      ac.options.ServiceName,
					},
					CABundle: caCert,
				},
			},
		},
	}
	if err := client.Delete(webhook.Name, nil); err != nil {
		serr, ok := err.(*apierrors.StatusError)
		if !ok || serr.ErrStatus.Code != http.StatusNotFound {
			glog.Warningf("Could not delete previously created AdmissionRegistration: %v", err)
		}
	}
	_, err := client.Create(webhook) // Update?
	return err
}

// ServeHTTP implements the external admission webhook for validating
// pilot configuration.
func (ac *AdmissionController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	glog.V(4).Infof("AdmissionController ServeHTTP request=%#v", r)

	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var review v1alpha1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		http.Error(w, fmt.Sprintf("could not decode body: %v", err), http.StatusBadRequest)
		return
	}

	status := ac.admit(&review)
	ar := v1alpha1.AdmissionReview{
		Status: *status,
	}

	glog.V(2).Info("AdmissionReview for %v: status=%v", review.Spec.Name, status)

	resp, err := json.Marshal(ar)
	if err != nil {
		http.Error(w, fmt.Sprintf("could encode response: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(resp); err != nil {
		http.Error(w, fmt.Sprintf("could write response: %v", err), http.StatusInternalServerError)
		return
	}
}

func watched(watchedNamespaces []string, namespace string) bool {
	for _, watched := range watchedNamespaces {
		if watched == metav1.NamespaceAll {
			return true
		} else if watched == namespace {
			return true
		}
		// else, keep searching
	}
	return false
}

func (ac *AdmissionController) admit(review *v1alpha1.AdmissionReview) *v1alpha1.AdmissionReviewStatus {
	makeErrorStatus := func(reason string, args ...interface{}) *v1alpha1.AdmissionReviewStatus {
		result := apierrors.NewBadRequest(fmt.Sprintf(reason, args...)).Status()
		return &v1alpha1.AdmissionReviewStatus{
			Result: &result,
		}
	}

	switch review.Spec.Operation {
	case admission.Create, admission.Update:
	default:
		glog.Warningf("Unsupported webhook operation %v", review.Spec.Operation)
		return &v1alpha1.AdmissionReviewStatus{Allowed: true}
	}

	var obj crd.IstioKind
	if err := yaml.Unmarshal(review.Spec.Object.Raw, &obj); err != nil {
		return makeErrorStatus("cannot decode configuration: %v", err)
	}

	if !watched(ac.options.ValidateNamespaces, obj.Namespace) {
		return &v1alpha1.AdmissionReviewStatus{Allowed: true}
	}

	schema, exists := ac.options.Descriptor.GetByType(crd.CamelCaseToKabobCase(obj.Kind))
	if !exists {
		return makeErrorStatus("unrecognized type %v", obj.Kind)
	}

	out, err := crd.ConvertObject(schema, &obj, ac.options.DomainSuffix)
	if err != nil {
		return makeErrorStatus("error decoding configuration: %v", err)
	}

	if err := schema.Validate(out.Spec); err != nil {
		return makeErrorStatus("configuration is invalid: %v", err)
	}

	return &v1alpha1.AdmissionReviewStatus{Allowed: true}
}
