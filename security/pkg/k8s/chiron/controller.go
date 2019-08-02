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

	"k8s.io/api/admissionregistration/v1beta1"

	"github.com/ghodss/yaml"
	"github.com/howeyc/fsnotify"
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

var (
	// TODO (lei-tang): the secret names, service names, ports may be moved to CLI.

	// WebhookServiceNames is service names of the webhooks.
	WebhookServiceNames = []string{
		"istio-sidecar-injector",
		"istio-galley",
	}

	// WebhookServicePorts is service ports of the webhooks. Each item corresponds to an item
	// at the same index in WebhookServiceNames.
	WebhookServicePorts = []int{
		443,
		443,
	}
)

// WebhookController manages the service accounts' secrets that contains Istio keys and certificates.
type WebhookController struct {
	core       corev1.CoreV1Interface
	admission  admissionv1.AdmissionregistrationV1beta1Interface
	certClient certclient.CertificatesV1beta1Interface

	minGracePeriod time.Duration
	// Length of the grace period for the certificate rotation.
	gracePeriodRatio float32

	// Controller and store for secret objects.
	scrtController cache.Controller
	scrtStore      cache.Store

	// The file path to the k8s CA certificate
	k8sCaCertFile string

	// The namespace of the webhook certificates
	namespace string

	// The file paths of MutatingWebhookConfiguration
	mutatingWebhookConfigFiles []string
	// The names of MutatingWebhookConfiguration
	mutatingWebhookConfigNames []string
	// The configuration of mutating webhook
	mutatingWebhookConfig *v1beta1.MutatingWebhookConfiguration

	// Watcher for the config files
	ConfigWatcher *fsnotify.Watcher

	// Current CA certificate
	CACert []byte

	mutex sync.RWMutex
}

// NewWebhookController returns a pointer to a newly constructed WebhookController instance.
func NewWebhookController(gracePeriodRatio float32, minGracePeriod time.Duration,
	core corev1.CoreV1Interface, admission admissionv1.AdmissionregistrationV1beta1Interface,
	certClient certclient.CertificatesV1beta1Interface, k8sCaCertFile, nameSpace string,
	mutatingWebhookConfigFiles, mutatingWebhookConfigNames []string) (*WebhookController, error) {

	if gracePeriodRatio < 0 || gracePeriodRatio > 1 {
		return nil, fmt.Errorf("grace period ratio %f should be within [0, 1]", gracePeriodRatio)
	}
	if gracePeriodRatio < recommendedMinGracePeriodRatio || gracePeriodRatio > recommendedMaxGracePeriodRatio {
		log.Warnf("grace period ratio %f is out of the recommended window [%.2f, %.2f]",
			gracePeriodRatio, recommendedMinGracePeriodRatio, recommendedMaxGracePeriodRatio)
	}

	c := &WebhookController{
		core:                       core,
		admission:                  admission,
		certClient:                 certClient,
		gracePeriodRatio:           gracePeriodRatio,
		minGracePeriod:             minGracePeriod,
		k8sCaCertFile:              k8sCaCertFile,
		namespace:                  nameSpace,
		mutatingWebhookConfigFiles: mutatingWebhookConfigFiles,
		mutatingWebhookConfigNames: mutatingWebhookConfigNames,
	}

	// read CA cert at the beginning of launching the controller and when the CA cert changes.
	_, err := reloadCaCert(c)
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

	// Create a watcher for the CA certificate such that when
	// the CA certificate changes, the webhook certificates are regenerated and
	// the CA bundles in webhook configurations get updated.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// Watch the parent directory of the target files so we can catch
	// symlink updates of k8s ConfigMaps volumes.
	// The files watched include the CA certificate file and the muatingwebhookconfiguration file,
	// which is a ConfigMap file mount.
	// In the prototype, only the first mutatingwebhookconfiguration is watched.
	for _, file := range []string{k8sCaCertFile, mutatingWebhookConfigFiles[0]} {
		watchDir, _ := filepath.Split(file)
		if err := watcher.Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}
	c.ConfigWatcher = watcher

	return c, nil
}

// Run starts the WebhookController until stopCh is notified.
func (wc *WebhookController) Run(stopCh chan struct{}) {
	// Create secrets containing certificates for webhooks
	for _, svcName := range WebhookServiceNames {
		wc.upsertSecret(wc.getWebhookSecretNameFromSvcname(svcName), wc.namespace)
	}

	host := fmt.Sprintf("%s.%s", WebhookServiceNames[0], wc.namespace)
	port := WebhookServicePorts[0]
	// In the prototype, Chiron only patches one webhook.
	// TODO (lei-tang): extend the prototype to manage all webhooks.
	go wc.checkAndCreateMutatingWebhook(host, port, stopCh)

	// Monitor the changes on mutatingwebhookconfiguration
	mutatingWebhookChangedCh := wc.monitorMutatingWebhookConfig(wc.mutatingWebhookConfigNames[0], stopCh)

	// Manage the secrets of webhooks
	go wc.scrtController.Run(stopCh)

	// upsertSecret to update and insert secret
	// it throws error if the secret cache is not synchronized, but the secret exists in the system
	cache.WaitForCacheSync(stopCh, wc.scrtController.HasSynced)

	// Watch for the CA certificate and mutatingwebhookconfiguration updates
	go wc.watchConfigChanges(mutatingWebhookChangedCh, stopCh)
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

func (wc *WebhookController) watchConfigChanges(mutatingWebhookChangedCh, stopCh chan struct{}) {
	// TODO (lei-tang): add the implementation of this function.
}

func (wc *WebhookController) getCACert() ([]byte, error) {
	wc.mutex.Lock()
	cp := append([]byte(nil), wc.CACert...)
	wc.mutex.Unlock()

	block, _ := pem.Decode(cp)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM encoded CA certificate")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return nil, fmt.Errorf("invalid ca certificate (%v), parsing error: %v", string(cp), err)
	}
	return cp, nil
}

// Create or update the mutatingwebhookconfiguration based on the config from rebuildMutatingWebhookConfig().
func (wc *WebhookController) createOrUpdateMutatingWebhookConfig() error {
	if wc.mutatingWebhookConfig == nil {
		return fmt.Errorf("mutatingwebhookconfiguration is nil")
	}

	client := wc.admission.MutatingWebhookConfigurations()
	updated, err := createOrUpdateMutatingWebhookConfigHelper(client, wc.mutatingWebhookConfig)
	if err != nil {
		return err
	} else if updated {
		log.Infof("%v mutatingwebhookconfiguration updated", wc.mutatingWebhookConfig.Name)
	}
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
	// The webhook in the prototype is a mutating webhook.
	// TODO (lei-tang): extend the demo webhook to all webhooks.
	// Try to create the initial webhook configuration (if it doesn't already exist).
	err := wc.rebuildMutatingWebhookConfig()
	if err == nil {
		createErr := wc.createOrUpdateMutatingWebhookConfig()
		if createErr != nil {
			log.Errorf("error when creating or updating muatingwebhookconfiguration: %v", createErr)
			return
		}
	} else {
		log.Errorf("error when rebuilding mutatingwebhookconfiguration: %v", err)
	}
}

// Run an informer that watches the changes of mutatingwebhookconfiguration.
func (wc *WebhookController) monitorMutatingWebhookConfig(mutatingWebhookConfigName string, stopC <-chan struct{}) chan struct{} {
	webhookChangedCh := make(chan struct{}, 1000)

	watchlist := cache.NewListWatchFromClient(
		wc.admission.RESTClient(),
		"mutatingwebhookconfigurations",
		"",
		fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", mutatingWebhookConfigName)))

	_, controller := cache.NewInformer(
		watchlist,
		&v1beta1.MutatingWebhookConfiguration{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(_ interface{}) {
				webhookChangedCh <- struct{}{}
			},
			UpdateFunc: func(prev, curr interface{}) {
				prevObj := prev.(*v1beta1.MutatingWebhookConfiguration)
				currObj := curr.(*v1beta1.MutatingWebhookConfiguration)
				if prevObj.ResourceVersion != currObj.ResourceVersion {
					webhookChangedCh <- struct{}{}
				}
			},
			DeleteFunc: func(_ interface{}) {
				webhookChangedCh <- struct{}{}
			},
		},
	)
	go controller.Run(stopC)
	return webhookChangedCh
}

// Rebuild the mutatingwebhookconfiguratio and save for subsequent calls to createOrUpdateWebhookConfig.
func (wc *WebhookController) rebuildMutatingWebhookConfig() error {
	caCert, err := wc.getCACert()
	if err != nil {
		return err
	}
	// In the prototype, only one mutating webhook is rebuilt
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

// Return the webhook secret name based on the service name
func (wc *WebhookController) getWebhookSecretNameFromSvcname(svcName string) string {
	return fmt.Sprintf("%s.%s", prefixWebhookSecretName, svcName)
}
