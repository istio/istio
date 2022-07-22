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

package chiron

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"
	certutil "istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
)

const (
	// IstioDNSSecretType is the Istio DNS secret annotation type
	IstioDNSSecretType = "istio.io/dns-key-and-cert"

	// For debugging, set the resync period to be a shorter period.
	secretResyncPeriod = 10 * time.Second

	recommendedMinGracePeriodRatio = 0.2
	recommendedMaxGracePeriodRatio = 0.8

	// The size of a private key for a leaf certificate.
	keySize = 2048

	// The number of retries when requesting to create secret.
	secretCreationRetry = 3

	// The number of tries for reading a certificate
	maxNumCertRead = 10

	// The interval for reading a certificate
	certReadInterval = 500 * time.Millisecond
)

var certWatchTimeout = 5 * time.Second

// WebhookController manages the service accounts' secrets that contains Istio keys and certificates.
type WebhookController struct {
	// The secret names of the services for which Chiron manage certs
	secretNames []string
	// The DNS names of the services for which Chiron manage certs
	dnsNames []string
	// The namespaces of the Secrets for which Chiron manage certs
	secretNamespace string

	// Current CA certificate
	CACert    []byte
	clientset clientset.Interface

	// certificate issuer
	certIssuer string
	// Controller and store for secret objects.
	scrtController cache.Controller
	scrtStore      cache.Store
	// The file path to the k8s CA certificate
	k8sCaCertFile  string
	minGracePeriod time.Duration
	certMutex      sync.RWMutex
	// Ratio of the grace period for the certificate rotation.
	gracePeriodRatio float32
	certUtil         certutil.CertUtil
}

// NewWebhookController returns a pointer to a newly constructed WebhookController instance.
func NewWebhookController(gracePeriodRatio float32, minGracePeriod time.Duration,
	client clientset.Interface,
	k8sCaCertFile string,
	secretNames, dnsNames []string,
	secretNamespace string, certIssuer string,
) (*WebhookController, error) {
	if gracePeriodRatio < 0 || gracePeriodRatio > 1 {
		return nil, fmt.Errorf("grace period ratio %f should be within [0, 1]", gracePeriodRatio)
	}
	if gracePeriodRatio < recommendedMinGracePeriodRatio || gracePeriodRatio > recommendedMaxGracePeriodRatio {
		log.Warnf("grace period ratio %f is out of the recommended window [%.2f, %.2f]",
			gracePeriodRatio, recommendedMinGracePeriodRatio, recommendedMaxGracePeriodRatio)
	}

	if len(secretNames) != len(dnsNames) {
		return nil, fmt.Errorf("the size of secret names must be the same as the size of dns names")
	}
	// Check secret names are unique
	set := make(map[string]bool) // New empty set
	for _, n := range secretNames {
		set[n] = true // Add
	}
	if len(set) != len(secretNames) {
		return nil, fmt.Errorf("the secret names must be unique")
	}

	c := &WebhookController{
		gracePeriodRatio: gracePeriodRatio,
		minGracePeriod:   minGracePeriod,
		k8sCaCertFile:    k8sCaCertFile,
		clientset:        client,
		secretNames:      secretNames,
		dnsNames:         dnsNames,
		secretNamespace:  secretNamespace,
		certUtil:         certutil.NewCertUtil(int(gracePeriodRatio * 100)),
		certIssuer:       certIssuer,
	}

	// read CA cert at the beginning of launching the controller.
	_, err := reloadCACert(c)
	if err != nil {
		return nil, err
	}
	if len(secretNames) == 0 {
		log.Warn("the input secrets are empty, no services to manage certificates for")
	} else {
		istioSecretSelector := fields.SelectorFromSet(map[string]string{"type": IstioDNSSecretType})
		scrtLW := cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "secrets", secretNamespace, istioSecretSelector)
		// The certificate rotation is handled by scrtUpdated().
		c.scrtStore, c.scrtController = cache.NewInformer(scrtLW, &v1.Secret{}, secretResyncPeriod, cache.ResourceEventHandlerFuncs{
			DeleteFunc: c.scrtDeleted,
			UpdateFunc: c.scrtUpdated,
		})
	}

	return c, nil
}

// Run starts the WebhookController until stopCh is notified.
func (wc *WebhookController) Run(stopCh <-chan struct{}) {
	// Create secrets containing certificates
	for i, secretName := range wc.secretNames {
		err := wc.upsertSecret(secretName, wc.dnsNames[i], wc.secretNamespace)
		if err != nil {
			log.Errorf("error when upserting secret (%v) in ns (%v): %v", secretName, wc.secretNamespace, err)
		}
	}

	if len(wc.secretNames) > 0 {
		// Manage the secrets
		go wc.scrtController.Run(stopCh)
		// upsertSecret to update and insert secret
		// it throws error if the secret cache is not synchronized, but the secret exists in the system.
		// Hence waiting for the cache is synced.
		if !kube.WaitForCacheSync(stopCh, wc.scrtController.HasSynced) {
			log.Error("failed to wait for cache sync")
		}
	}
}

func (wc *WebhookController) upsertSecret(secretName, dnsName, secretNamespace string) error {
	secret := &v1.Secret{
		Data: map[string][]byte{},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: nil,
			Name:        secretName,
			Namespace:   secretNamespace,
		},
		Type: IstioDNSSecretType,
	}

	existingSecret, err := wc.clientset.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err == nil && existingSecret != nil {
		log.Debugf("upsertSecret(): the secret (%v) in namespace (%v) exists, return",
			secretName, secretNamespace)
		// Do nothing for existing secrets. Rotating expiring certs are handled by the `scrtUpdated` method.
		return nil
	}

	requestedLifetime := time.Duration(0)
	// Now we know the secret does not exist yet. So we create a new one.
	chain, key, caCert, err := GenKeyCertK8sCA(wc.clientset, dnsName, wc.k8sCaCertFile, wc.certIssuer, true, requestedLifetime)
	if err != nil {
		log.Errorf("failed to generate key and certificate for secret %v in namespace %v (error %v)",
			secretName, secretNamespace, err)
		return err
	}
	secret.Data = map[string][]byte{
		ca.CertChainFile:  chain,
		ca.PrivateKeyFile: key,
		ca.RootCertFile:   caCert,
	}

	// We retry several times when create secret to mitigate transient network failures.
	for i := 0; i < secretCreationRetry; i++ {
		_, err = wc.clientset.CoreV1().Secrets(secretNamespace).Create(context.TODO(), secret, metav1.CreateOptions{})
		if err == nil || errors.IsAlreadyExists(err) {
			if errors.IsAlreadyExists(err) {
				log.Infof("Istio secret \"%s\" in namespace \"%s\" already exists", secretName, secretNamespace)
			}
			break
		}
		log.Warnf("failed to create secret in attempt %v/%v, (error: %s)", i+1, secretCreationRetry, err)
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

func (wc *WebhookController) scrtDeleted(obj any) {
	log.Debugf("enter WebhookController.scrtDeleted()")
	scrt, ok := obj.(*v1.Secret)
	if !ok {
		log.Warnf("failed to convert to secret object: %v", obj)
		return
	}

	scrtName := scrt.Name
	if wc.isWebhookSecret(scrtName, scrt.GetNamespace()) {
		log.Infof("re-create deleted Istio secret %s in namespace %s", scrtName, scrt.GetNamespace())
		dnsName, found := wc.getDNSName(scrtName)
		if !found {
			log.Errorf("failed to find the DNS name of the secret: %v", scrtName)
			return
		}
		err := wc.upsertSecret(scrtName, dnsName, scrt.GetNamespace())
		if err != nil {
			log.Errorf("re-create deleted Istio secret %s in namespace %s failed: %v",
				scrtName, scrt.GetNamespace(), err)
		}
	}
}

// scrtUpdated() is the callback function for update event. It handles
// the certificate rotations.
func (wc *WebhookController) scrtUpdated(oldObj, newObj any) {
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

	certBytes := scrt.Data[ca.CertChainFile]
	_, err := util.ParsePemEncodedCertificate(certBytes)
	if err != nil {
		log.Warnf("failed to parse certificates in secret %s/%s (error: %v), refreshing the secret.",
			namespace, name, err)
		if err = wc.refreshSecret(scrt); err != nil {
			log.Error(err)
		}

		return
	}

	_, waitErr := wc.certUtil.GetWaitTime(certBytes, time.Now(), wc.minGracePeriod)

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
	if waitErr != nil || !bytes.Equal(caCert, scrt.Data[ca.RootCertFile]) {
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

	dnsName, found := wc.getDNSName(scrtName)
	if !found {
		return fmt.Errorf("failed to find the service name for the secret (%v) to refresh", scrtName)
	}

	requestedLifetime := time.Duration(0)
	chain, key, caCert, err := GenKeyCertK8sCA(wc.clientset, dnsName, wc.k8sCaCertFile, wc.certIssuer, true, requestedLifetime)
	if err != nil {
		return err
	}

	scrt.Data[ca.CertChainFile] = chain
	scrt.Data[ca.PrivateKeyFile] = key
	scrt.Data[ca.RootCertFile] = caCert

	_, err = wc.clientset.CoreV1().Secrets(namespace).Update(context.TODO(), scrt, metav1.UpdateOptions{})
	return err
}

// Return whether the input secret name is a Webhook secret
func (wc *WebhookController) isWebhookSecret(name, namespace string) bool {
	for _, n := range wc.secretNames {
		if name == n && namespace == wc.secretNamespace {
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

// Get the DNS name for the secret. Return the DNS name and whether it is found.
func (wc *WebhookController) getDNSName(secretName string) (string, bool) {
	for i, name := range wc.secretNames {
		if name == secretName {
			return wc.dnsNames[i], true
		}
	}
	return "", false
}
