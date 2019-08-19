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
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/howeyc/fsnotify"
	"k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/api/core/v1"
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
	IstioSecretType = "istio.io/webhook-key-and-cert"

	// The ID/name for the private key file.
	PrivateKeyID = "key.pem"

	// For debugging, set the resync period to be a shorter period.
	secretResyncPeriod = 10 * time.Second
	// secretResyncPeriod = time.Minute

	recommendedMinGracePeriodRatio = 0.2
	recommendedMaxGracePeriodRatio = 0.8
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
			return nil, err
		}
		watchDir, _ := filepath.Split(files[i])
		if err := (*watchers[i]).Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", files[i], err)
		}
	}

	return c, nil
}

// Run starts the WebhookController until stopCh is notified.
func (wc *WebhookController) Run(stopCh chan struct{}) {
	// Create secrets containing certificates for webhooks
	for _, svcName := range wc.mutatingWebhookServiceNames {
		wc.upsertSecret(wc.getWebhookSecretNameFromSvcname(svcName), wc.namespace)
	}
	for _, svcName := range wc.validatingWebhookServiceNames {
		wc.upsertSecret(wc.getWebhookSecretNameFromSvcname(svcName), wc.namespace)
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

func (wc *WebhookController) upsertSecret(secretName, secretNamespace string) {
	// TODO (lei-tang): add the implementation of this function.
}

func (wc *WebhookController) scrtDeleted(obj interface{}) {
	// TODO (lei-tang): add the implementation of this function.
}

// scrtUpdated() is the callback function for update event. It handles
// the certificate rotations.
func (wc *WebhookController) scrtUpdated(oldObj, newObj interface{}) {
	// TODO (lei-tang): add the implementation of this function.
}

func (wc *WebhookController) watchConfigChanges(mutatingWebhookChangedCh, validatingWebhookChangedCh,
	stopCh chan struct{}) {
	// TODO (lei-tang): add the implementation of this function.
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
	// TODO (lei-tang): add the implementation of this function.
	return nil
}

// Rebuild the validatingwebhookconfiguration and save it for subsequent calls to createOrUpdateWebhookConfig.
func (wc *WebhookController) rebuildValidatingWebhookConfig() error {
	// TODO (lei-tang): add the implementation of this function.
	return nil
}

// Return the webhook secret name based on the service name
func (wc *WebhookController) getWebhookSecretNameFromSvcname(svcName string) string {
	return fmt.Sprintf("%s.%s", prefixWebhookSecretName, svcName)
}
