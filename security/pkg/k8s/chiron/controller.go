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
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/client-go/kubernetes"

	"github.com/howeyc/fsnotify"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	certclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	istioutil "istio.io/istio/pkg/util"
	crl "istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/pkg/listwatch"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

/* #nosec: disable gas linter */
const (
	// The Istio secret annotation type
	IstioSecretType = "istio.io/key-and-cert"

	// The ID/name for the certificate chain file.
	CertChainID = "cert-chain.pem"
	// The ID/name for the private key file.
	PrivateKeyID = "key.pem"
	// The ID/name for the CA root certificate file.
	RootCertID = "root-cert.pem"
	// The key to specify corresponding service account in the annotation of K8s secrets.
	ServiceAccountNameAnnotationKey = "istio.io/service-account.name"

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

	// The path storing the CA certificate of the k8s apiserver
	caCertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// The delay introduced to debounce the CA cert events
	watchDebounceDelay = 100 * time.Millisecond
)

var (
	// WebhookServiceAccounts is service accounts for the webhooks.
	WebhookServiceAccounts = []string{
		// This service account is the service account of a demo webhook.
		// It is for running the prototype on the demo webhook.
		"istio-protomutate-service-account",
		// TODO (lei-tang): remove demo service account and enable the following webhook service accounts
		// "istio-sidecar-injector-service-account",
		// "istio-galley-service-account",
	}

	// WebhookServiceNames is service names of the webhooks. Each item corresponds to an item
	// at the same index in WebhookServiceAccounts.
	WebhookServiceNames = []string{
		// This is a service name of a demo webhook. It is for running the prototype on the demo webhook.
		"protomutate",
		// TODO (lei-tang): remove demo service name and enable the following webhook service names
		//		"istio-sidecar-injector",
		//		"istio-galley",
	}

	// WebhookServicePorts is service ports of the webhooks. Each item corresponds to an item
	// at the same index in WebhookServiceNames.
	WebhookServicePorts = []int{
		// This is a service port created as a demo of the prototype
		443,
		// TODO (lei-tang): remove demo service port and enable the following webhook service ports
		//	443,
		//	443,
	}
)

// WebhookController manages the service accounts' secrets that contains Istio keys and certificates.
type WebhookController struct {
	k8sClient      *kubernetes.Clientset
	core           corev1.CoreV1Interface
	minGracePeriod time.Duration
	// Length of the grace period for the certificate rotation.
	gracePeriodRatio float32

	// DNS-enabled serviceAccount.namespace to service pair
	dnsNames map[string]*crl.DNSNameEntry

	// Controller and store for secret objects.
	scrtController cache.Controller
	scrtStore      cache.Store

	certClient certclient.CertificatesV1beta1Interface

	// The namespace of the webhook certificates
	namespace string

	// MutatingWebhookConfiguration
	mutatingWebhookConfigName string
	// The name of the webhook in the MutatingWebhookConfiguration
	mutatingWebhookName string

	// Watcher for the CA certificate
	CaCertWatcher *fsnotify.Watcher

	// Current CA certificate
	curCACert []byte

	mutex sync.RWMutex
}

// NewWebhookController returns a pointer to a newly constructed WebhookController instance.
func NewWebhookController(gracePeriodRatio float32, minGracePeriod time.Duration, k8sClient *kubernetes.Clientset,
	core corev1.CoreV1Interface, certClient certclient.CertificatesV1beta1Interface,
	dnsNames map[string]*crl.DNSNameEntry, nameSpace, mutatingWebhookConfigName, mutatingWebhookName string) (*WebhookController, error) {

	if gracePeriodRatio < 0 || gracePeriodRatio > 1 {
		return nil, fmt.Errorf("grace period ratio %f should be within [0, 1]", gracePeriodRatio)
	}
	if gracePeriodRatio < recommendedMinGracePeriodRatio || gracePeriodRatio > recommendedMaxGracePeriodRatio {
		log.Warnf("grace period ratio %f is out of the recommended window [%.2f, %.2f]",
			gracePeriodRatio, recommendedMinGracePeriodRatio, recommendedMaxGracePeriodRatio)
	}

	c := &WebhookController{
		gracePeriodRatio:          gracePeriodRatio,
		minGracePeriod:            minGracePeriod,
		k8sClient:                 k8sClient,
		core:                      core,
		dnsNames:                  dnsNames,
		certClient:                certClient,
		namespace:                 nameSpace,
		mutatingWebhookConfigName: mutatingWebhookConfigName,
		mutatingWebhookName:       mutatingWebhookName,
	}

	caCert, err := readCACert()
	if err != nil {
		log.Errorf("failed to read CA certificate: %v", err)
		return nil, err
	}
	c.setCurCACert(caCert)

	namespaces := []string{nameSpace}

	istioSecretSelector := fields.SelectorFromSet(map[string]string{"type": IstioSecretType}).String()
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

	// Create a watcher for the CA certificate such that when
	// the CA certificate changes, the webhook certificates are regenerated and
	// the CA bundles in webhook configurations get updated.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// watch the parent directory of the target files so we can catch
	// symlink updates of k8s ConfigMaps volumes.
	for _, file := range []string{caCertPath} {
		watchDir, _ := filepath.Split(file)
		if err := watcher.Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}
	c.CaCertWatcher = watcher

	return c, nil
}

// Run starts the WebhookController until stopCh is notified.
func (wc *WebhookController) Run(stopCh chan struct{}) {
	// In the prototype, Chiron only manages one webhook.
	// TODO (lei-tang): extend the prototype to manage all webhooks.
	host := fmt.Sprintf("%s.%s", WebhookServiceNames[0], wc.namespace)
	port := WebhookServicePorts[0]
	go wc.checkAndPatchMutatingWebhook(host, port, stopCh)

	// Create secrets containing certificates for webhooks
	for _, saName := range WebhookServiceAccounts {
		wc.upsertSecret(saName, wc.namespace)
	}
	// Manage the secrets of webhooks
	go wc.scrtController.Run(stopCh)

	// upsertSecret to update and insert secret
	// it throws error if the secret cache is not synchronized, but the secret exists in the system
	cache.WaitForCacheSync(stopCh, wc.scrtController.HasSynced)

	// Watch for the CA certificate updates
	go wc.watchCACert(stopCh)
}

func (wc *WebhookController) upsertSecret(saName, saNamespace string) {
	secret := ca.BuildSecret(saName, crl.GetSecretName(saName), saNamespace, nil, nil, nil, nil, nil, IstioSecretType)

	_, exists, err := wc.scrtStore.Get(secret)
	if err != nil {
		log.Errorf("failed to get secret from the store (error %v)", err)
	}

	if exists {
		// Do nothing for existing secrets. Rotating expiring certs are handled by the `scrtUpdated` method.
		return
	}

	// Now we know the secret does not exist yet. So we create a new one.
	chain, key, err := GenKeyCertK8sCA(wc, saName, saNamespace)
	if err != nil {
		log.Errorf("failed to generate key and certificate for service account %q in namespace %q (error %v)",
			saName, saNamespace, err)
		return
	}
	secret.Data = map[string][]byte{
		CertChainID:  chain,
		PrivateKeyID: key,
		RootCertID:   wc.getCurCACert(),
	}

	// We retry several times when create secret to mitigate transient network failures.
	for i := 0; i < secretCreationRetry; i++ {
		_, err = wc.core.Secrets(saNamespace).Create(secret)
		if err == nil || errors.IsAlreadyExists(err) {
			if errors.IsAlreadyExists(err) {
				log.Infof("Istio secret for service account \"%s\" in namespace \"%s\" already exists", saName, saNamespace)
			}
			break
		} else {
			log.Errorf("Failed to create secret in attempt %v/%v, (error: %s)", i+1, secretCreationRetry, err)
		}
		time.Sleep(time.Second)
	}

	if err != nil && !errors.IsAlreadyExists(err) {
		log.Errorf("Failed to create secret for service account \"%s\"  (error: %s), retries %v times",
			saName, err, secretCreationRetry)
		return
	}

	log.Infof("Istio secret for service account \"%s\" in namespace \"%s\" has been created", saName, saNamespace)
}

func (wc *WebhookController) deleteSecret(saName, saNamespace string) {
	err := wc.core.Secrets(saNamespace).Delete(crl.GetSecretName(saName), nil)
	// kube-apiserver returns NotFound error when the secret is successfully deleted.
	if err == nil || errors.IsNotFound(err) {
		log.Infof("Istio secret for service account \"%s\" in namespace \"%s\" has been deleted", saName, saNamespace)
		return
	}

	log.Errorf("Failed to delete Istio secret for service account \"%s\" in namespace \"%s\" (error: %s)",
		saName, saNamespace, err)
}

func (wc *WebhookController) scrtDeleted(obj interface{}) {
	scrt, ok := obj.(*v1.Secret)
	if !ok {
		log.Warnf("Failed to convert to secret object: %v", obj)
		return
	}

	saName := scrt.Annotations[ServiceAccountNameAnnotationKey]
	if _, err := wc.core.ServiceAccounts(scrt.GetNamespace()).Get(saName, metav1.GetOptions{}); err == nil {
		if wc.isWebhookSA(saName, scrt.GetNamespace()) {
			log.Errorf("Re-create deleted Istio secret for existing service account %s.", saName)
			wc.upsertSecret(saName, scrt.GetNamespace())
		}
	}
}

// scrtUpdated() is the callback function for update event. It handles
// the certificate rotations.
func (wc *WebhookController) scrtUpdated(oldObj, newObj interface{}) {
	scrt, ok := newObj.(*v1.Secret)
	if !ok {
		log.Warnf("Failed to convert to secret object: %v", newObj)
		return
	}
	namespace := scrt.GetNamespace()
	name := scrt.GetName()
	// Only handle webhook secret update events
	if !wc.isWebhookSecret(name, namespace) {
		return
	}

	certBytes := scrt.Data[CertChainID]
	cert, err := util.ParsePemEncodedCertificate(certBytes)
	if err != nil {
		log.Warnf("Failed to parse certificates in secret %s/%s (error: %v), refreshing the secret.",
			namespace, name, err)
		if err = wc.refreshSecret(scrt); err != nil {
			log.Errora(err)
		}

		return
	}

	certLifeTimeLeft := time.Until(cert.NotAfter)
	certLifeTime := cert.NotAfter.Sub(cert.NotBefore)
	// TODO(myidpt): we may introduce a minimum gracePeriod, without making the config too complex.
	// Because time.Duration only takes int type, multiply gracePeriodRatio by 1000 and then divide it.
	gracePeriod := time.Duration(wc.gracePeriodRatio*1000) * certLifeTime / 1000
	if gracePeriod < wc.minGracePeriod {
		log.Warnf("gracePeriod (%v * %f) = %v is less than minGracePeriod %v. Apply minGracePeriod.",
			certLifeTime, wc.gracePeriodRatio, gracePeriod, wc.minGracePeriod)
		gracePeriod = wc.minGracePeriod
	}

	// Refresh the secret if 1) the certificate contained in the secret is about
	// to expire, or 2) the root certificate in the secret is different than the
	// one held by the ca (this may happen when the CA is restarted and
	// a new self-signed CA cert is generated).
	if certLifeTimeLeft < gracePeriod || !bytes.Equal(wc.getCurCACert(), scrt.Data[RootCertID]) {
		log.Infof("Refreshing secret %s/%s, either the leaf certificate is about to expire "+
			"or the root certificate is outdated", namespace, name)

		if err = wc.refreshSecret(scrt); err != nil {
			log.Errorf("Failed to update secret %s/%s (error: %s)", namespace, name, err)
		}
	}
}

// refreshSecret is an inner func to refresh cert secrets when necessary
func (wc *WebhookController) refreshSecret(scrt *v1.Secret) error {
	namespace := scrt.GetNamespace()
	saName := scrt.Annotations[ServiceAccountNameAnnotationKey]

	chain, key, err := GenKeyCertK8sCA(wc, saName, namespace)
	if err != nil {
		return err
	}

	scrt.Data[CertChainID] = chain
	scrt.Data[PrivateKeyID] = key
	scrt.Data[RootCertID] = wc.getCurCACert()

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

// Return whether the input service account name is a Webhook service account
func (wc *WebhookController) isWebhookSA(name, namespace string) bool {
	for _, n := range WebhookServiceAccounts {
		if n == name && namespace == wc.namespace {
			return true
		}
	}
	return false
}

// Return whether the input secret name is a webhook secret
func (wc *WebhookController) isWebhookSecret(name, namespace string) bool {
	for _, n := range WebhookServiceAccounts {
		if crl.GetSecretName(n) == name && namespace == wc.namespace {
			return true
		}
	}
	return false
}

func (wc *WebhookController) watchCACert(stopCh chan struct{}) {
	var timerC <-chan time.Time
	for {
		select {
		case <-timerC:
			// Update webhook certificates and WebhookConfigurations
			timerC = nil

			caCert, err := readCACert()
			if err != nil {
				log.Errorf("failed to read CA certificate: %v", err)
				break
			}
			if !bytes.Equal(caCert, wc.getCurCACert()) {
				log.Debug("CA cert changed, update webhook certs and webhook configuration")
				wc.setCurCACert(caCert)
				// Update the webhook certificate
				for _, name := range WebhookServiceAccounts {
					wc.upsertSecret(name, wc.namespace)
				}
				// Patch the WebhookConfiguration
				// TODO (lei-tang): extend the demo webhook to all webhooks
				doPatch(wc.k8sClient, wc.mutatingWebhookConfigName, wc.mutatingWebhookName, wc.getCurCACert())
			}
		case event := <-wc.CaCertWatcher.Event:
			// use a timer to debounce configuration updates
			if (event.IsModify() || event.IsCreate()) && timerC == nil {
				timerC = time.After(watchDebounceDelay)
			}
		case err := <-wc.CaCertWatcher.Error:
			log.Errorf("CA certificate watcher error: %v", err)
		case <-stopCh:
			return
		}
	}
}

func (wc *WebhookController) getCurCACert() []byte {
	wc.mutex.Lock()
	cp := append([]byte(nil), wc.curCACert...)
	wc.mutex.Unlock()
	return cp
}

func (wc *WebhookController) setCurCACert(cert []byte) {
	wc.mutex.Lock()
	wc.curCACert = append([]byte(nil), cert...)
	wc.mutex.Unlock()
}

func (wc *WebhookController) checkAndPatchMutatingWebhook(host string, port int, stopCh chan struct{}) {
	// Check the webhook service status. Only configure webhook if the webhook service is available.
	for {
		if isTCPReachable(host, port) {
			log.Info("the webhook service is reachable")
			break
		}
		select {
		case <-stopCh:
			log.Debugf("webhook controlller is stopped")
			return
		default:
			log.Debugf("the webhook service is unreachable, check again later ...")
			time.Sleep(2 * time.Second)
		}
	}
	// The webhook in the prototype is a mutating webhook.
	err := wc.patchMutatingCertLoop(wc.k8sClient, wc.mutatingWebhookConfigName, wc.mutatingWebhookName, stopCh)
	if err != nil {
		// Abort if failed to patch the mutating webhook
		log.Fatalf("failed to patch the mutating webhook: %v", err)
	}
}

func (wc *WebhookController) patchMutatingCertLoop(client *kubernetes.Clientset, webhookConfigName, webhookName string, stopCh <-chan struct{}) error {
	// Chiron configures WebhookConfiguration
	if err := istioutil.PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
		webhookConfigName, webhookName, wc.getCurCACert()); err != nil {
		return err
	}

	shouldPatch := make(chan struct{})

	watchlist := cache.NewListWatchFromClient(
		client.AdmissionregistrationV1beta1().RESTClient(),
		"mutatingwebhookconfigurations",
		"",
		fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", webhookConfigName)))

	_, controller := cache.NewInformer(
		watchlist,
		&v1beta1.MutatingWebhookConfiguration{},
		0,
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				config := newObj.(*v1beta1.MutatingWebhookConfiguration)
				// If the MutatingWebhookConfiguration changes and the CA bundle differs from current CA cert,
				// patch the CA bundle.
				for i, w := range config.Webhooks {
					if w.Name == webhookName && !bytes.Equal(config.Webhooks[i].ClientConfig.CABundle, wc.getCurCACert()) {
						log.Infof("Detected a change in CABundle, patching MutatingWebhookConfiguration again")
						shouldPatch <- struct{}{}
						break
					}
				}
			},
		},
	)
	go controller.Run(stopCh)

	go func() {
		for range shouldPatch {
			doPatch(client, webhookConfigName, webhookName, wc.getCurCACert())
		}
	}()

	return nil
}
