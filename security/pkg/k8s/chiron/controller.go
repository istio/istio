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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/howeyc/fsnotify"
	"k8s.io/api/admissionregistration/v1beta1"
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

	// The ID/name for the certificate chain file.
	CertChainID = "cert-chain.pem"
	// The ID/name for the private key file.
	PrivateKeyID = "key.pem"
	// The ID/name for the CA root certificate file.
	RootCertID = "root-cert.pem"

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
	// The file paths of MutatingWebhookConfiguration
	mutatingWebhookConfigFiles []string
	// The names of MutatingWebhookConfiguration
	mutatingWebhookConfigNames []string
	// The names of the services of mutating webhooks
	mutatingWebhookServiceNames []string
	// The ports of the services of mutating webhooks
	mutatingWebhookServicePorts []int
	// The file paths of ValidatingWebhookConfiguration
	validatingWebhookConfigFiles []string
	// The names of ValidatingWebhookConfiguration
	validatingWebhookConfigNames []string
	// The names of the services of validating webhooks
	validatingWebhookServiceNames []string
	// The ports of the services of validating webhooks
	validatingWebhookServicePorts []int

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
	// The configuration of mutating webhook
	mutatingWebhookConfig *v1beta1.MutatingWebhookConfiguration
	// The configuration of validating webhook
	validatingWebhookConfig *v1beta1.ValidatingWebhookConfiguration
	// Watcher for the k8s CA cert file
	K8sCaCertWatcher *fsnotify.Watcher
	// Watcher for the mutatingwebhook config file
	MutatingWebhookFileWatcher *fsnotify.Watcher
	// Watcher for the validatingwebhook config file
	ValidatingWebhookFileWatcher *fsnotify.Watcher
	certMutex                    sync.RWMutex
	// Length of the grace period for the certificate rotation.
	gracePeriodRatio                  float32
	deleteWebhookConfigurationsOnExit bool
}

// NewWebhookController returns a pointer to a newly constructed WebhookController instance.
func NewWebhookController(deleteWebhookConfigurationsOnExit bool, gracePeriodRatio float32, minGracePeriod time.Duration,
	core corev1.CoreV1Interface, admission admissionv1.AdmissionregistrationV1beta1Interface,
	certClient certclient.CertificatesV1beta1Interface, k8sCaCertFile, nameSpace string,
	mutatingWebhookConfigFiles, mutatingWebhookConfigNames, mutatingWebhookServiceNames []string,
	mutatingWebhookServicePorts []int, validatingWebhookConfigFiles, validatingWebhookConfigNames,
	validatingWebhookServiceNames []string, validatingWebhookServicePorts []int) (*WebhookController, error) {
	if gracePeriodRatio < 0 || gracePeriodRatio > 1 {
		return nil, fmt.Errorf("grace period ratio %f should be within [0, 1]", gracePeriodRatio)
	}
	if gracePeriodRatio < recommendedMinGracePeriodRatio || gracePeriodRatio > recommendedMaxGracePeriodRatio {
		log.Warnf("grace period ratio %f is out of the recommended window [%.2f, %.2f]",
			gracePeriodRatio, recommendedMinGracePeriodRatio, recommendedMaxGracePeriodRatio)
	}

	c := &WebhookController{
		deleteWebhookConfigurationsOnExit: deleteWebhookConfigurationsOnExit,
		gracePeriodRatio:                  gracePeriodRatio,
		minGracePeriod:                    minGracePeriod,
		k8sCaCertFile:                     k8sCaCertFile,
		core:                              core,
		admission:                         admission,
		certClient:                        certClient,
		namespace:                         nameSpace,
		mutatingWebhookConfigFiles:        mutatingWebhookConfigFiles,
		mutatingWebhookConfigNames:        mutatingWebhookConfigNames,
		mutatingWebhookServiceNames:       mutatingWebhookServiceNames,
		mutatingWebhookServicePorts:       mutatingWebhookServicePorts,
		validatingWebhookConfigFiles:      validatingWebhookConfigFiles,
		validatingWebhookConfigNames:      validatingWebhookConfigNames,
		validatingWebhookServiceNames:     validatingWebhookServiceNames,
		validatingWebhookServicePorts:     validatingWebhookServicePorts,
	}

	// read CA cert at the beginning of launching the controller and when the CA cert changes.
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

	watchers := []**fsnotify.Watcher{&c.K8sCaCertWatcher, &c.MutatingWebhookFileWatcher, &c.ValidatingWebhookFileWatcher}
	// Create a watcher such that when the file changes, the event is detected.
	// Each watcher corresponds to a file.
	// Watch the parent directory of the target files so we can catch
	// symlink updates of k8s ConfigMaps volumes.
	// The files watched include the CA certificate file and the webhookconfiguration files,
	// which are ConfigMap file mounts.
	// In the prototype, only the first webhookconfiguration is watched.
	files := []string{k8sCaCertFile, mutatingWebhookConfigFiles[0], validatingWebhookConfigFiles[0]}
	for i := range watchers {
		*watchers[i], err = fsnotify.NewWatcher()
		if err != nil {
			for _, w := range watchers {
				if *w != nil {
					(*w).Close()
				}
			}
			return nil, err
		}
		watchDir, _ := filepath.Split(files[i])
		if err := (*watchers[i]).Watch(watchDir); err != nil {
			for _, w := range watchers {
				if *w != nil {
					(*w).Close()
				}
			}
			return nil, fmt.Errorf("could not watch %v: %v", files[i], err)
		}
	}

	return c, nil
}

// Run starts the WebhookController until stopCh is notified.
func (wc *WebhookController) Run(stopCh chan struct{}) {
	// Create secrets containing certificates for webhooks
	for _, svcName := range wc.mutatingWebhookServiceNames {
		err := wc.upsertSecret(wc.getWebhookSecretNameFromSvcname(svcName), wc.namespace)
		if err != nil {
			log.Errorf("error when upserting svc (%v) in ns (%v): %v", svcName, wc.namespace, err)
		}
	}
	for _, svcName := range wc.validatingWebhookServiceNames {
		err := wc.upsertSecret(wc.getWebhookSecretNameFromSvcname(svcName), wc.namespace)
		if err != nil {
			log.Errorf("error when upserting svc (%v) in ns (%v): %v", svcName, wc.namespace, err)
		}
	}

	// Currently, Chiron only patches one mutating webhook and one validating webhook.
	var mutatingWebhookChangedCh chan struct{}
	// Delete the existing webhookconfiguration, if any.
	err := wc.deleteMutatingWebhookConfig(wc.mutatingWebhookConfigNames[0])
	if err != nil {
		log.Infof("deleting mutating webhook config %v returns: %v",
			wc.mutatingWebhookConfigNames[0], err)
	}
	hostMutate := fmt.Sprintf("%s.%s", wc.mutatingWebhookServiceNames[0], wc.namespace)
	go wc.checkAndCreateMutatingWebhook(hostMutate, wc.mutatingWebhookServicePorts[0], stopCh)
	// Only the first mutatingWebhookConfigNames is supported
	mutatingWebhookChangedCh = wc.monitorMutatingWebhookConfig(wc.mutatingWebhookConfigNames[0], stopCh)

	var validatingWebhookChangedCh chan struct{}
	// Delete the existing webhookconfiguration, if any.
	err = wc.deleteValidatingWebhookConfig(wc.validatingWebhookConfigNames[0])
	if err != nil {
		log.Infof("deleting validatingwebhook config %v returns: %v",
			wc.validatingWebhookConfigNames[0], err)
	}

	hostValidate := fmt.Sprintf("%s.%s", wc.validatingWebhookServiceNames[0], wc.namespace)
	go wc.checkAndCreateValidatingWebhook(hostValidate, wc.validatingWebhookServicePorts[0], stopCh)
	// Only the first validatingWebhookConfigNames is supported
	validatingWebhookChangedCh = wc.monitorValidatingWebhookConfig(wc.validatingWebhookConfigNames[0], stopCh)

	// Manage the secrets of webhooks
	go wc.scrtController.Run(stopCh)

	// upsertSecret to update and insert secret
	// it throws error if the secret cache is not synchronized, but the secret exists in the system
	cache.WaitForCacheSync(stopCh, wc.scrtController.HasSynced)

	// Watch for the CA certificate and webhookconfiguration updates
	go wc.watchConfigChanges(mutatingWebhookChangedCh, validatingWebhookChangedCh, stopCh)
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
		CertChainID:  chain,
		PrivateKeyID: key,
		RootCertID:   caCert,
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
			log.Errorf("failed to create secret in attempt %v/%v, (error: %s)", i+1, secretCreationRetry, err)
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
	// TODO (lei-tang): add the implementation of this function.
}

// scrtUpdated() is the callback function for update event. It handles
// the certificate rotations.
func (wc *WebhookController) scrtUpdated(oldObj, newObj interface{}) {
	// TODO (lei-tang): add the implementation of this function.
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

func (wc *WebhookController) watchConfigChanges(mutatingWebhookChangedCh, validatingWebhookChangedCh,
	stopCh chan struct{}) {
	// TODO (lei-tang): add the implementation of this function.
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
		if wc.getWebhookSecretNameFromSvcname(name) == secretName {
			return name, true
		}
	}
	for _, name := range wc.validatingWebhookServiceNames {
		if wc.getWebhookSecretNameFromSvcname(name) == secretName {
			return name, true
		}
	}
	return "", false
}

// Delete the mutatingwebhookconfiguration.
func (wc *WebhookController) deleteMutatingWebhookConfig(configName string) error {
	// TODO (lei-tang): add the implementation of this function.
	return nil
}

// Delete the validatingwebhookconfiguration.
func (wc *WebhookController) deleteValidatingWebhookConfig(configName string) error {
	// TODO (lei-tang): add the implementation of this function.
	return nil
}

func (wc *WebhookController) checkAndCreateMutatingWebhook(host string, port int, stopCh chan struct{}) {
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
			log.Debugf("the webhook service at (%v, %v) is unreachable, check again later ...", host, port)
			time.Sleep(2 * time.Second)
		}
	}

	// Try to create the initial webhook configuration.
	err := wc.rebuildMutatingWebhookConfig()
	if err == nil {
		createErr := createOrUpdateMutatingWebhookConfig(wc)
		if createErr != nil {
			log.Errorf("error when creating or updating muatingwebhookconfiguration: %v", createErr)
			return
		}
	} else {
		log.Errorf("error when rebuilding mutatingwebhookconfiguration: %v", err)
	}
}

func (wc *WebhookController) checkAndCreateValidatingWebhook(host string, port int, stopCh chan struct{}) {
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

	// Try to create the initial webhook configuration.
	err := wc.rebuildValidatingWebhookConfig()
	if err == nil {
		createErr := createOrUpdateValidatingWebhookConfig(wc)
		if createErr != nil {
			log.Errorf("error when creating or updating validatingwebhookconfiguration: %v", createErr)
			return
		}
	} else {
		log.Errorf("error when rebuilding validatingwebhookconfiguration: %v", err)
	}
}

// Run an informer that watches the changes of mutatingwebhookconfiguration.
func (wc *WebhookController) monitorMutatingWebhookConfig(webhookConfigName string, stopC <-chan struct{}) chan struct{} {
	// TODO (lei-tang): add the implementation of this function.
	return nil
}

// Run an informer that watches the changes of validatingwebhookconfiguration.
func (wc *WebhookController) monitorValidatingWebhookConfig(webhookConfigName string, stopC <-chan struct{}) chan struct{} {
	// TODO (lei-tang): add the implementation of this function.
	return nil
}

// Rebuild the mutatingwebhookconfiguration and save it for subsequent calls to createOrUpdateWebhookConfig.
func (wc *WebhookController) rebuildMutatingWebhookConfig() error {
	caCert, err := wc.getCACert()
	if err != nil {
		return err
	}
	// In the prototype, only one mutating webhook is rebuilt
	// The size of mutatingWebhookConfigFiles and mutatingWebhookConfigNames are checked in main.
	webhookConfig, err := rebuildMutatingWebhookConfigHelper(
		caCert,
		wc.mutatingWebhookConfigFiles[0],
		wc.mutatingWebhookConfigNames[0],
	)
	if err != nil {
		log.Errorf("failed to build mutatingwebhookconfiguration: %v", err)
		return err
	}
	wc.mutatingWebhookConfig = webhookConfig

	// print the mutatingwebhookconfiguration as YAML
	var configYAML string
	b, err := yaml.Marshal(wc.mutatingWebhookConfig)

	if err == nil {
		configYAML = string(b)
		log.Debugf("%v mutatingwebhookconfiguration is rebuilt: \n%v",
			wc.mutatingWebhookConfig.Name, configYAML)
		return nil
	}
	log.Errorf("error to marshal mutatingwebhookconfiguration %v: %v",
		wc.mutatingWebhookConfig.Name, err)
	return err
}

// Rebuild the validatingwebhookconfiguration and save it for subsequent calls to createOrUpdateWebhookConfig.
func (wc *WebhookController) rebuildValidatingWebhookConfig() error {
	caCert, err := wc.getCACert()
	if err != nil {
		return err
	}
	// In the prototype, only one validating webhook is rebuilt.
	// The size of validatingWebhookConfigFiles and validatingWebhookConfigNames are checked in main.
	webhookConfig, err := rebuildValidatingWebhookConfigHelper(
		caCert,
		wc.validatingWebhookConfigFiles[0],
		wc.validatingWebhookConfigNames[0],
	)
	if err != nil {
		log.Errorf("failed to build validatingwebhookconfiguration: %v", err)
		return err
	}
	wc.validatingWebhookConfig = webhookConfig

	// print the validatingwebhookconfiguration as YAML
	var configYAML string
	b, err := yaml.Marshal(wc.validatingWebhookConfig)

	if err == nil {
		configYAML = string(b)
		log.Debugf("%v validatingwebhookconfiguration is rebuilt: \n%v",
			wc.validatingWebhookConfig.Name, configYAML)
		return nil
	}
	log.Errorf("error to marshal validatingwebhookconfiguration %v: %v",
		wc.validatingWebhookConfig.Name, err)
	return err
}

// Return the webhook secret name based on the service name
func (wc *WebhookController) getWebhookSecretNameFromSvcname(svcName string) string {
	return fmt.Sprintf("%s.%s", prefixWebhookSecretName, svcName)
}
