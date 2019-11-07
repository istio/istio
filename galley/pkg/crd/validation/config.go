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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/ghodss/yaml"
	"github.com/howeyc/fsnotify"
	"k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
	admissionregistration "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("validation", "CRD validation debugging", 0)

type createInformerWebhookSource func(cl clientset.Interface, name string) cache.ListerWatcher

var (
	defaultCreateInformerWebhookSource = func(cl clientset.Interface, name string) cache.ListerWatcher {
		return cache.NewListWatchFromClient(
			cl.AdmissionregistrationV1beta1().RESTClient(),
			"validatingwebhookconfigurations",
			"",
			fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", name)))
	}
)

// WebhookConfigController implements the validating admission webhook for validating Istio configuration.
type WebhookConfigController struct {
	configWatcher        *fsnotify.Watcher
	webhookParameters    *WebhookParameters
	ownerRefs            []metav1.OwnerReference
	webhookConfiguration *v1beta1.ValidatingWebhookConfiguration

	// test hook for informers
	createInformerWebhookSource createInformerWebhookSource
}

// Run an informer that watches the current webhook configuration
// for changes.
func (whc *WebhookConfigController) monitorWebhookChanges(stopC <-chan struct{}) chan struct{} {
	webhookChangedCh := make(chan struct{}, 1000)
	_, controller := cache.NewInformer(
		whc.createInformerWebhookSource(whc.webhookParameters.Clientset, whc.webhookParameters.WebhookName),
		&v1beta1.ValidatingWebhookConfiguration{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(_ interface{}) {
				webhookChangedCh <- struct{}{}
			},
			UpdateFunc: func(prev, curr interface{}) {
				prevObj := prev.(*v1beta1.ValidatingWebhookConfiguration)
				currObj := curr.(*v1beta1.ValidatingWebhookConfiguration)
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

func (whc *WebhookConfigController) createOrUpdateWebhookConfig() (retry bool) {
	if whc.webhookConfiguration == nil {
		scope.Error("validatingwebhookconfiguration update failed: no configuration loaded")
		reportValidationConfigUpdateError(errors.New("no configuration loaded"))
		return false
	}

	client := whc.webhookParameters.Clientset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	updated, err := createOrUpdateWebhookConfigHelper(client, whc.webhookConfiguration)
	if err != nil {
		scope.Errorf("%v validatingwebhookconfiguration update failed: %v", whc.webhookConfiguration.Name, err)
		reportValidationConfigUpdateError(fmt.Errorf("createOrUpdate failed: %v", kerrors.ReasonForError(err)))
		return true
	}

	if updated {
		scope.Infof("%v validatingwebhookconfiguration updated", whc.webhookConfiguration.Name)
		reportValidationConfigUpdate()
	} else {
		scope.Infof("%v validatingwebhookconfiguration unchanged, no update needed", whc.webhookConfiguration.Name)
	}
	return false
}

// Create the specified validatingwebhookconfiguration resource or, if the resource
// already exists, update it's contents with the desired state.
func createOrUpdateWebhookConfigHelper(
	client admissionregistration.ValidatingWebhookConfigurationInterface,
	webhookConfiguration *v1beta1.ValidatingWebhookConfiguration,
) (bool, error) {
	current, err := client.Get(webhookConfiguration.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if _, createErr := client.Create(webhookConfiguration); createErr != nil {
				return false, createErr
			}
			return true, nil
		}
		return false, err
	}

	// Minimize the diff between the actual vs. desired state. Only copy the relevant fields
	// that we want reconciled and ignore everything else, e.g. labels, selectors.
	updated := current.DeepCopyObject().(*v1beta1.ValidatingWebhookConfiguration)
	updated.Webhooks = webhookConfiguration.Webhooks
	updated.OwnerReferences = webhookConfiguration.OwnerReferences

	if !reflect.DeepEqual(updated, current) {
		_, err := client.Update(updated)
		return true, err
	}
	return false, nil
}

// Delete validatingwebhookconfiguration if the validation is disabled
func (whc *WebhookConfigController) deleteWebhookConfig() (retry bool) {
	client := whc.webhookParameters.Clientset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()

	deleted, err := deleteWebhookConfigHelper(client, whc.webhookParameters.WebhookName)
	if err != nil {
		scope.Errorf("%v validatingwebhookconfiguration delete failed: %v", whc.webhookParameters.WebhookName, err)
		reportValidationConfigDeleteError(fmt.Errorf("delete failed: %v", kerrors.ReasonForError(err)))
		return true
	}
	scope.Infof("Delete %v validatingwebhookconfiguration is %v", whc.webhookParameters.WebhookName, deleted)
	return false
}

// Delete validatingwebhookconfiguration if exists. otherwise, do nothing
func deleteWebhookConfigHelper(
	client admissionregistration.ValidatingWebhookConfigurationInterface,
	webhookName string,
) (bool, error) {
	_, err := client.Get(webhookName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	err = client.Delete(webhookName, &metav1.DeleteOptions{})
	if err != nil {
		return false, err
	}
	return true, nil
}

// Rebuild the validatingwebhookconfiguration and save for subsequent calls to createOrUpdateWebhookConfig.
func (whc *WebhookConfigController) rebuildWebhookConfig() error {
	webhookConfig, err := rebuildWebhookConfigHelper(
		whc.webhookParameters.CACertFile,
		whc.webhookParameters.WebhookConfigFile,
		whc.webhookParameters.WebhookName,
		whc.ownerRefs)
	if err != nil {
		reportValidationConfigLoadError(err)
		scope.Errorf("validatingwebhookconfiguration (re)load failed: %v", err)
		return err
	}
	whc.webhookConfiguration = webhookConfig

	// pretty-print the validatingwebhookconfiguration as YAML
	var webhookYAML string
	if b, err := yaml.Marshal(whc.webhookConfiguration); err == nil {
		webhookYAML = string(b)
	}
	scope.Infof("%v validatingwebhookconfiguration (re)loaded: \n%v",
		whc.webhookConfiguration.Name, webhookYAML)

	reportValidationConfigLoad()

	return nil
}

// Load the CA Cert PEM from the input reader. This also verifies that the certificate is a validate x509 cert.
func loadCaCertPem(in io.Reader) ([]byte, error) {
	caCertPemBytes, err := ioutil.ReadAll(in)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(caCertPemBytes)
	if block == nil {
		return nil, errors.New("could not decode pem")
	}
	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("ca bundle contains wrong pem type: %q", block.Type)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return nil, fmt.Errorf("ca bundle contains invalid x509 certificate: %v", err)
	}
	return caCertPemBytes, nil
}

// Rebuild the desired validatingwebhookconfiguration from the specified CA
// and webhook config files. This also ensures the OwnerReferences is set
// so that the cluster-scoped validatingwebhookconfiguration is properly
// cleaned up when istio-galley is deleted.
func rebuildWebhookConfigHelper(
	caFile, webhookConfigFile, webhookName string,
	ownerRefs []metav1.OwnerReference,
) (*v1beta1.ValidatingWebhookConfiguration, error) {
	// load and validate configuration
	webhookConfigData, err := ioutil.ReadFile(webhookConfigFile)
	if err != nil {
		return nil, err
	}
	var webhookConfig v1beta1.ValidatingWebhookConfiguration
	if err := yaml.Unmarshal(webhookConfigData, &webhookConfig); err != nil {
		return nil, fmt.Errorf("could not decode validatingwebhookconfiguration from %v: %v",
			webhookConfigFile, err)
	}

	// fill in missing defaults to minimize desired vs. actual diffs later.
	for i := 0; i < len(webhookConfig.Webhooks); i++ {
		if webhookConfig.Webhooks[i].FailurePolicy == nil {
			failurePolicy := v1beta1.Fail
			webhookConfig.Webhooks[i].FailurePolicy = &failurePolicy
		}
		if webhookConfig.Webhooks[i].NamespaceSelector == nil {
			webhookConfig.Webhooks[i].NamespaceSelector = &metav1.LabelSelector{}
		}
	}

	// the webhook name is fixed at startup time
	webhookConfig.Name = webhookName

	// update ownerRefs so configuration is cleaned up when the galley's namespace is deleted.
	webhookConfig.OwnerReferences = ownerRefs

	in, err := os.Open(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read ca bundle from %v: %v", caFile, err)
	}
	defer in.Close() // nolint: errcheck

	caPem, err := loadCaCertPem(in)
	if err != nil {
		return nil, err
	}

	// patch the ca-cert into the user provided configuration
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caPem
	}

	return &webhookConfig, nil
}

// NewWebhookConfigController manages validating webhook configuration.
func NewWebhookConfigController(p WebhookParameters) (*WebhookConfigController, error) {

	// Configuration must be updated whenever the caBundle changes. watch the parent directory of
	// the target files so we can catch symlink updates of k8s secrets.
	fileWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	for _, file := range []string{p.CACertFile, p.WebhookConfigFile} {
		watchDir, _ := filepath.Split(file)
		if err := fileWatcher.Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}

	whc := &WebhookConfigController{
		configWatcher:               fileWatcher,
		webhookParameters:           &p,
		createInformerWebhookSource: defaultCreateInformerWebhookSource,
	}

	galleyNamespace, err := whc.webhookParameters.Clientset.CoreV1().Namespaces().Get(
		whc.webhookParameters.DeploymentAndServiceNamespace, metav1.GetOptions{})
	if err != nil {
		scope.Warnf("Could not find %s namespace to set ownerRef. "+
			"The validatingwebhookconfiguration must be deleted manually",
			whc.webhookParameters.DeploymentAndServiceNamespace)
	} else {
		whc.ownerRefs = []metav1.OwnerReference{
			*metav1.NewControllerRef(
				galleyNamespace,
				corev1.SchemeGroupVersion.WithKind("Namespace"),
			),
		}
	}

	return whc, nil
}

//reconcile monitors the keycert and webhook configuration changes, rebuild and reconcile the configuration
func (whc *WebhookConfigController) reconcile(stopCh <-chan struct{}) {
	defer whc.configWatcher.Close() // nolint: errcheck

	// Try to create the initial webhook configuration (if it doesn't
	// already exist). Setup a persistent monitor to reconcile the
	// configuration if the observed configuration doesn't match
	// the desired configuration.
	var retryAfterSetup bool
	if err := whc.rebuildWebhookConfig(); err == nil {
		retryAfterSetup = whc.createOrUpdateWebhookConfig()
	}
	webhookChangedCh := whc.monitorWebhookChanges(stopCh)

	// use a timer to debounce file updates
	var configTimerC <-chan time.Time

	if retryAfterSetup {
		configTimerC = time.After(retryUpdateAfterFailureTimeout)
	}

	var retrying bool
	for {
		select {
		case <-configTimerC:
			configTimerC = nil

			// rebuild the desired configuration and reconcile with the
			// existing configuration.
			if err := whc.rebuildWebhookConfig(); err == nil {
				if retry := whc.createOrUpdateWebhookConfig(); retry {
					configTimerC = time.After(retryUpdateAfterFailureTimeout)
					if !retrying {
						retrying = true
						log.Infof("webhook create/update failed - retrying every %v until success", retryUpdateAfterFailureTimeout)
					}
				} else if retrying {
					log.Infof("Retried create/update succeeded")
					retrying = false
				}
			}
		case <-webhookChangedCh:
			var retry bool
			if whc.webhookParameters.EnableValidation {
				// reconcile the desired configuration
				if retry = whc.createOrUpdateWebhookConfig(); retry && !retrying {
					log.Infof("webhook create/update failed - retrying every %v until success", retryUpdateAfterFailureTimeout)
				}
			} else {
				if retry = whc.deleteWebhookConfig(); retry && !retrying {
					log.Infof("webhook delete failed - retrying every %v until success", retryUpdateAfterFailureTimeout)
				}
			}
			retrying = retry
			if retry {
				time.AfterFunc(retryUpdateAfterFailureTimeout, func() { webhookChangedCh <- struct{}{} })
			}
		case event, more := <-whc.configWatcher.Event:
			if more && (event.IsModify() || event.IsCreate()) && configTimerC == nil {
				configTimerC = time.After(watchDebounceDelay)
			}
		case err := <-whc.configWatcher.Error:
			scope.Errorf("configWatcher error: %v", err)
		case <-stopCh:
			return
		}
	}
}

// ReconcileWebhookConfiguration reconciles the ValidatingWebhookConfiguration when the webhook server is ready
func ReconcileWebhookConfiguration(webhookServerReady, stopCh <-chan struct{},
	vc *WebhookParameters, kubeConfig string) {

	clientset, err := kube.CreateClientset(kubeConfig, "")
	if err != nil {
		log.Fatalf("could not create k8s clientset: %v", err)
	}
	vc.Clientset = clientset

	whc, err := NewWebhookConfigController(*vc)
	if err != nil {
		log.Fatalf("cannot create validation webhook config: %v", err)
	}

	if vc.EnableValidation {
		//wait for galley endpoint to be available before register ValidatingWebhookConfiguration
		<-webhookServerReady
	}
	whc.reconcile(stopCh)

}
