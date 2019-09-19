// Copyright 2019 Istio Authors
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

package chiron

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	admissionv1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	certclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/security/pkg/listwatch"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

type WebhookType int

const (
	MutatingWebhook WebhookType = iota
	ValidatingWebhook
)

/* #nosec: disable gas linter */
const (
	// The prefix of webhook secret name
	prefixWebhookSecretName = "istio.webhook"

	// The Istio webhook secret annotation type
	IstioWebhookSecretType = "istio.io/webhook-key-and-cert"

	// For debugging, set the resync period to be a shorter period.
	secretResyncPeriod = 10 * time.Second
	// secretResyncPeriod = time.Minute

	recommendedMinGracePeriodRatio = 0.2
	recommendedMaxGracePeriodRatio = 0.8

	// The size of a private key for a leaf certificate.
	keySize = 2048

	// The number of retries when requesting to create secret.
	secretCreationRetry = 3

	// The interval for reading a certificate
	certReadInterval = 500 * time.Millisecond
	// The number of tries for reading a certificate
	maxNumCertRead = 20
)

// WebhookController manages the service accounts' secrets that contains Istio keys and certificates.
type WebhookController struct {
	// The names of the services of mutating webhooks
	mutatingWebhookServiceNames []string
	// The names of the services of validating webhooks
	validatingWebhookServiceNames []string

	// Current CA certificate
	CACert     []byte
	core       corev1.CoreV1Interface
	admission  admissionv1.AdmissionregistrationV1beta1Interface
	certClient certclient.CertificatesV1beta1Interface
	// Controller and store for secret objects.
	scrtController cache.Controller
	scrtStore      cache.Store
	// The file path to the k8s CA certificate
	k8sCaCertFile string
	// The namespace of the webhook certificates
	namespace      string
	minGracePeriod time.Duration
	certMutex      sync.RWMutex
	// Length of the grace period for the certificate rotation.
	gracePeriodRatio float32
}

// NewWebhookController returns a pointer to a newly constructed WebhookController instance.
func NewWebhookController(gracePeriodRatio float32, minGracePeriod time.Duration,
	core corev1.CoreV1Interface, admission admissionv1.AdmissionregistrationV1beta1Interface,
	certClient certclient.CertificatesV1beta1Interface, k8sCaCertFile, nameSpace string,
	mutatingWebhookServiceNames []string,
	validatingWebhookServiceNames []string) (*WebhookController, error) {
	if gracePeriodRatio < 0 || gracePeriodRatio > 1 {
		return nil, fmt.Errorf("grace period ratio %f should be within [0, 1]", gracePeriodRatio)
	}
	if gracePeriodRatio < recommendedMinGracePeriodRatio || gracePeriodRatio > recommendedMaxGracePeriodRatio {
		log.Warnf("grace period ratio %f is out of the recommended window [%.2f, %.2f]",
			gracePeriodRatio, recommendedMinGracePeriodRatio, recommendedMaxGracePeriodRatio)
	}

	c := &WebhookController{
		gracePeriodRatio:              gracePeriodRatio,
		minGracePeriod:                minGracePeriod,
		k8sCaCertFile:                 k8sCaCertFile,
		core:                          core,
		admission:                     admission,
		certClient:                    certClient,
		namespace:                     nameSpace,
		mutatingWebhookServiceNames:   mutatingWebhookServiceNames,
		validatingWebhookServiceNames: validatingWebhookServiceNames,
	}

	// read CA cert at the beginning of launching the controller.
	_, err := reloadCACert(c)
	if err != nil {
		return nil, err
	}

	namespaces := []string{nameSpace}

	istioSecretSelector := fields.SelectorFromSet(map[string]string{"type": IstioWebhookSecretType}).String()
	scrtLW := listwatch.MultiNamespaceListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.FieldSelector = istioSecretSelector
				return core.Secrets(namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.FieldSelector = istioSecretSelector
				return core.Secrets(namespace).Watch(options)
			},
		}
	})
	// The certificate rotation is handled by scrtUpdated().
	c.scrtStore, c.scrtController =
		cache.NewInformer(scrtLW, &v1.Secret{}, secretResyncPeriod, cache.ResourceEventHandlerFuncs{
			DeleteFunc: c.scrtDeleted,
			UpdateFunc: c.scrtUpdated,
		})

	return c, nil
}

// Run starts the WebhookController until stopCh is notified.
func (wc *WebhookController) Run(stopCh chan struct{}) {
	// Create secrets containing certificates for webhooks
	for _, svcName := range wc.mutatingWebhookServiceNames {
		err := wc.upsertSecret(wc.getWebhookSecretNameFromSvcName(svcName), wc.namespace)
		if err != nil {
			log.Errorf("error when upserting svc (%v) in ns (%v): %v", svcName, wc.namespace, err)
		}
	}
	for _, svcName := range wc.validatingWebhookServiceNames {
		err := wc.upsertSecret(wc.getWebhookSecretNameFromSvcName(svcName), wc.namespace)
		if err != nil {
			log.Errorf("error when upserting svc (%v) in ns (%v): %v", svcName, wc.namespace, err)
		}
	}

	// Manage the secrets of webhooks
	go wc.scrtController.Run(stopCh)

	// upsertSecret to update and insert secret
	// it throws error if the secret cache is not synchronized, but the secret exists in the system.
	// Hence waiting for the cache is synced.
	cache.WaitForCacheSync(stopCh, wc.scrtController.HasSynced)
}

func (wc *WebhookController) upsertSecret(secretName, secretNamespace string) error {
	secret := &v1.Secret{
		Data: map[string][]byte{},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: nil,
			Name:        secretName,
			Namespace:   secretNamespace,
		},
		Type: IstioWebhookSecretType,
	}

	existingSecret, err := wc.core.Secrets(secretNamespace).Get(secretName, metav1.GetOptions{})
	if err == nil && existingSecret != nil {
		log.Debugf("upsertSecret(): the secret (%v) in namespace (%v) exists, return",
			secretName, secretNamespace)
		// Do nothing for existing secrets. Rotating expiring certs are handled by the `scrtUpdated` method.
		return nil
	}

	svcName, found := wc.getServiceName(secretName)
	if !found {
		log.Errorf("failed to find the service name for the secret (%v) to insert", secretName)
		return fmt.Errorf("failed to find the service name for the secret (%v) to insert", secretName)
	}

	// Now we know the secret does not exist yet. So we create a new one.
	chain, key, caCert, err := genKeyCertK8sCA(wc, secretName, secretNamespace, svcName)
	if err != nil {
		log.Errorf("failed to generate key and certificate for secret %v in namespace %v (error %v)",
			secretName, secretNamespace, err)
		return err
	}
	secret.Data = map[string][]byte{
		ca.CertChainID:  chain,
		ca.PrivateKeyID: key,
		ca.RootCertID:   caCert,
	}

	// We retry several times when create secret to mitigate transient network failures.
	for i := 0; i < secretCreationRetry; i++ {
		_, err = wc.core.Secrets(secretNamespace).Create(secret)
		if err == nil || errors.IsAlreadyExists(err) {
			if errors.IsAlreadyExists(err) {
				log.Infof("Istio secret \"%s\" in namespace \"%s\" already exists", secretName, secretNamespace)
			}
			break
		} else {
			log.Warnf("failed to create secret in attempt %v/%v, (error: %s)", i+1, secretCreationRetry, err)
		}
		time.Sleep(time.Second)
	}

	if err != nil && !errors.IsAlreadyExists(err) {
		log.Errorf("failed to create secret \"%s\" in namespace \"%s\" (error: %s), retries %v times",
			secretName, secretNamespace, err, secretCreationRetry)
		return err
	}

	log.Infof("Istio secret \"%s\" in namespace \"%s\" has been created", secretName, secretNamespace)
	return nil
}

func (wc *WebhookController) scrtDeleted(obj interface{}) {
	log.Debugf("enter WebhookController.scrtDeleted()")
	scrt, ok := obj.(*v1.Secret)
	if !ok {
		log.Warnf("Failed to convert to secret object: %v", obj)
		return
	}

	scrtName := scrt.Name
	if wc.isWebhookSecret(scrtName, scrt.GetNamespace()) {
		log.Infof("Re-create deleted Istio secret %s in namespace %s", scrtName, scrt.GetNamespace())
		err := wc.upsertSecret(scrtName, scrt.GetNamespace())
		if err != nil {
			log.Errorf("Re-create deleted Istio secret %s in namespace %s failed: %v",
				scrtName, scrt.GetNamespace(), err)
		}
	}
}

// scrtUpdated() is the callback function for update event. It handles
// the certificate rotations.
func (wc *WebhookController) scrtUpdated(oldObj, newObj interface{}) {
	log.Debugf("enter WebhookController.scrtUpdated()")
	scrt, ok := newObj.(*v1.Secret)
	if !ok {
		log.Warnf("failed to convert to secret object: %v", newObj)
		return
	}
	namespace := scrt.GetNamespace()
	name := scrt.GetName()
	// Only handle webhook secret update events
	if !wc.isWebhookSecret(name, namespace) {
		return
	}

	certBytes := scrt.Data[ca.CertChainID]
	cert, err := util.ParsePemEncodedCertificate(certBytes)
	if err != nil {
		log.Warnf("failed to parse certificates in secret %s/%s (error: %v), refreshing the secret.",
			namespace, name, err)
		if err = wc.refreshSecret(scrt); err != nil {
			log.Errora(err)
		}

		return
	}

	certLifeTimeLeft := time.Until(cert.NotAfter)
	certLifeTime := cert.NotAfter.Sub(cert.NotBefore)
	// Because time.Duration only takes int type, multiply gracePeriodRatio by 1000 and then divide it.
	gracePeriod := time.Duration(wc.gracePeriodRatio*1000) * certLifeTime / 1000
	if gracePeriod < wc.minGracePeriod {
		log.Warnf("gracePeriod (%v * %f) = %v is less than minGracePeriod %v. Apply minGracePeriod.",
			certLifeTime, wc.gracePeriodRatio, gracePeriod, wc.minGracePeriod)
		gracePeriod = wc.minGracePeriod
	}

	// Refresh the secret if 1) the certificate contained in the secret is about
	// to expire, or 2) the root certificate in the secret is different than the
	// one held by the CA (this may happen when the CA is restarted and
	// a new self-signed CA cert is generated).
	// The secret will be periodically inspected, so an update to the CA certificate
	// will eventually lead to the update of workload certificates.
	caCert, err := wc.getCACert()
	if err != nil {
		log.Errorf("failed to get CA certificate: %v", err)
		return
	}
	if certLifeTimeLeft < gracePeriod || !bytes.Equal(caCert, scrt.Data[ca.RootCertID]) {
		log.Infof("refreshing secret %s/%s, either the leaf certificate is about to expire "+
			"or the root certificate is outdated", namespace, name)

		if err = wc.refreshSecret(scrt); err != nil {
			log.Errorf("failed to update secret %s/%s (error: %s)", namespace, name, err)
		}
	}
}

// refreshSecret is an inner func to refresh cert secrets when necessary
func (wc *WebhookController) refreshSecret(scrt *v1.Secret) error {
	namespace := scrt.GetNamespace()
	scrtName := scrt.Name

	svcName, found := wc.getServiceName(scrtName)
	if !found {
		return fmt.Errorf("failed to find the service name for the secret (%v) to refresh", scrtName)
	}

	chain, key, caCert, err := genKeyCertK8sCA(wc, scrtName, namespace, svcName)
	if err != nil {
		return err
	}

	scrt.Data[ca.CertChainID] = chain
	scrt.Data[ca.PrivateKeyID] = key
	scrt.Data[ca.RootCertID] = caCert

	_, err = wc.core.Secrets(namespace).Update(scrt)
	return err
}

// Clean up the CSR
func (wc *WebhookController) cleanUpCertGen(csrName string) error {
	// Delete CSR
	log.Debugf("delete CSR: %v", csrName)
	err := wc.certClient.CertificateSigningRequests().Delete(csrName, nil)
	if err != nil {
		log.Errorf("failed to delete CSR (%v): %v", csrName, err)
		return err
	}
	return nil
}

// Return whether the input secret name is a Webhook secret
func (wc *WebhookController) isWebhookSecret(name, namespace string) bool {
	for _, n := range wc.mutatingWebhookServiceNames {
		if name == wc.getWebhookSecretNameFromSvcName(n) && namespace == wc.namespace {
			return true
		}
	}
	for _, n := range wc.validatingWebhookServiceNames {
		if name == wc.getWebhookSecretNameFromSvcName(n) && namespace == wc.namespace {
			return true
		}
	}
	return false
}

// Get the CA cert. K8sCaCertWatcher handles the update of CA cert.
func (wc *WebhookController) getCACert() ([]byte, error) {
	wc.certMutex.Lock()
	cp := append([]byte(nil), wc.CACert...)
	wc.certMutex.Unlock()

	block, _ := pem.Decode(cp)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM encoded CA certificate")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return nil, fmt.Errorf("invalid ca certificate (%v), parsing error: %v", string(cp), err)
	}
	return cp, nil
}

// Get the service name for the secret. Return the service name and whether it is found.
func (wc *WebhookController) getServiceName(secretName string) (string, bool) {
	for _, name := range wc.mutatingWebhookServiceNames {
		if wc.getWebhookSecretNameFromSvcName(name) == secretName {
			return name, true
		}
	}
	for _, name := range wc.validatingWebhookServiceNames {
		if wc.getWebhookSecretNameFromSvcName(name) == secretName {
			return name, true
		}
	}
	return "", false
}

// Return the webhook secret name based on the service name
func (wc *WebhookController) getWebhookSecretNameFromSvcName(svcName string) string {
	return fmt.Sprintf("%s.%s", prefixWebhookSecretName, svcName)
}
