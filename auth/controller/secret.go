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

package controller

import (
	"reflect"
	"time"

	"github.com/golang/glog"

	"istio.io/auth/certmanager"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
)

/* #nosec: disable gas linter */
const (
	secretNamePrefix = "istio."
	istioSecretType  = "istio.io/key-and-cert"
)

// SecretController manages the service accounts' secrets that contains Istio keys and certificates.
type SecretController struct {
	ca   certmanager.CertificateAuthority
	core corev1.CoreV1Interface

	// Controller and store for service account objects.
	saController cache.Controller
	saStore      cache.Store

	// Controller and store for secret objects.
	scrtController cache.Controller
	scrtStore      cache.Store
}

// NewSecretController returns a pointer to a newly constructed SecretController instance.
func NewSecretController(ca certmanager.CertificateAuthority, core corev1.CoreV1Interface) *SecretController {
	c := &SecretController{
		ca:   ca,
		core: core,
	}

	saLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return core.ServiceAccounts(metav1.NamespaceAll).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return core.ServiceAccounts(metav1.NamespaceAll).Watch(options)
		},
	}
	rehf := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.saAdded,
		DeleteFunc: c.saDeleted,
		UpdateFunc: c.saUpdated,
	}
	c.saStore, c.saController = cache.NewInformer(saLW, &v1.ServiceAccount{}, time.Minute, rehf)

	istioSecretSelector := fields.SelectorFromSet(map[string]string{"type": istioSecretType}).String()
	scrtLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = istioSecretSelector
			return core.Secrets(metav1.NamespaceAll).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = istioSecretSelector
			return core.Secrets(metav1.NamespaceAll).Watch(options)
		},
	}
	c.scrtStore, c.scrtController =
		cache.NewInformer(scrtLW, &v1.Secret{}, time.Minute, cache.ResourceEventHandlerFuncs{})

	return c
}

// Run starts the SecretController until stopCh is closed.
func (sc *SecretController) Run(stopCh chan struct{}) {
	go sc.scrtController.Run(stopCh)
	go sc.saController.Run(stopCh)
	<-stopCh
}

// Handles the event where a service account is added.
func (sc *SecretController) saAdded(obj interface{}) {
	acct := obj.(*v1.ServiceAccount)
	sc.upsertSecret(acct.GetName(), acct.GetNamespace())
}

// Handles the event where a service account is deleted.
func (sc *SecretController) saDeleted(obj interface{}) {
	acct := obj.(*v1.ServiceAccount)
	sc.deleteSecret(acct.GetName(), acct.GetNamespace())
}

// Handles the event where a service account is updated.
func (sc *SecretController) saUpdated(oldObj, curObj interface{}) {
	if reflect.DeepEqual(oldObj, curObj) {
		// Nothing is changed. The method is invoked by periodical re-sync with the apiserver.
		return
	}
	oldSa := oldObj.(*v1.ServiceAccount)
	curSa := curObj.(*v1.ServiceAccount)

	curName := curSa.GetName()
	curNamespace := curSa.GetNamespace()
	oldName := oldSa.GetName()
	oldNamespace := oldSa.GetNamespace()

	// We only care the name and namespace of a service account.
	if curName != oldName || curNamespace != oldNamespace {
		sc.deleteSecret(oldName, oldNamespace)
		sc.upsertSecret(curName, curNamespace)

		glog.Infof("Service account \"%s\" in namespace \"%s\" has been updated to \"%s\" in namespace \"%s\"",
			oldName, oldNamespace, curName, curNamespace)
	}
}

func (sc *SecretController) upsertSecret(saName, saNamespace string) {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getSecretName(saName),
			Namespace: saNamespace,
		},
		Type: istioSecretType,
	}

	_, exists, err := sc.scrtStore.Get(secret)
	if err != nil {
		glog.Errorf("Failed to get secret from the store (error %v)", err)
	}

	if exists {
		// TODO (Yang Guan): https://github.com/istio/auth/issues/56
		// For an existing secret, we should check the cert expiration time and refresh it when necessary.
		return
	}

	// Now we know the secret does not exist yet. So we create a new one.
	chain, key := sc.ca.Generate(saName, saNamespace)
	rootCert := sc.ca.GetRootCertificate()
	secret.Data = map[string][]byte{
		"cert-chain.pem": chain,
		"key.pem":        key,
		"root-cert.pem":  rootCert,
	}
	_, err = sc.core.Secrets(saNamespace).Create(secret)
	if err != nil {
		glog.Errorf("Failed to create secret (error: %s)", err)
		return
	}

	glog.Infof("Istio secret for service account \"%s\" in namespace \"%s\" has been created", saName, saNamespace)
}

func (sc *SecretController) deleteSecret(saName, saNamespace string) {
	err := sc.core.Secrets(saNamespace).Delete(getSecretName(saName), nil)
	// kube-apiserver returns NotFound error when the secret is successfully deleted.
	if err == nil || errors.IsNotFound(err) {
		glog.Infof("Istio secret for service account \"%s\" in namespace \"%s\" has been deleted", saName, saNamespace)
		return
	}

	glog.Errorf("Failed to delete Istio secret for service account \"%s\" in namespace \"%s\" (error: %s)",
		saName, saNamespace, err)
}

func getSecretName(saName string) string {
	return secretNamePrefix + saName
}
