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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
)

/* #nosec: disable gas linter */
const secretNamePrefix = "istio."

// SecretController manages the service accounts' secrets that contains Istio keys and certificates.
type SecretController struct {
	ca           certmanager.CertificateAuthority
	core         corev1.CoreV1Interface
	saController cache.Controller
	saStore      cache.Store
}

// NewSecretController returns a pointer to a newly constructed SecretController instance.
func NewSecretController(ca certmanager.CertificateAuthority, core corev1.CoreV1Interface) *SecretController {
	c := &SecretController{
		ca:   ca,
		core: core,
	}

	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return core.ServiceAccounts(metav1.NamespaceAll).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return core.ServiceAccounts(metav1.NamespaceAll).Watch(options)
		},
	}
	rehf := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addFunc,
		DeleteFunc: c.deleteFunc,
		UpdateFunc: c.updateFunc,
	}
	c.saStore, c.saController = cache.NewInformer(lw, &v1.ServiceAccount{}, time.Minute, rehf)

	return c
}

// Run starts the SecretController until stopCh is closed.
func (sc *SecretController) Run(stopCh chan struct{}) {
	go sc.saController.Run(stopCh)
	<-stopCh
}

func (sc *SecretController) addFunc(obj interface{}) {
	acct := obj.(*v1.ServiceAccount)
	sc.createSecret(acct.GetName(), acct.GetNamespace())
}

func (sc *SecretController) deleteFunc(obj interface{}) {
	acct := obj.(*v1.ServiceAccount)
	sc.deleteSecret(acct.GetName(), acct.GetNamespace())
}

func (sc *SecretController) updateFunc(oldObj, curObj interface{}) {
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
		sc.createSecret(curName, curNamespace)

		glog.Infof("Service account \"%s\" in namespace \"%s\" has been updated to \"%s\" in namespace \"%s\"",
			oldName, oldNamespace, curName, curNamespace)
	}
}

func (sc *SecretController) createSecret(saName, saNamespace string) {
	cert, key := sc.ca.Generate(saName, saNamespace)
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: getSecretName(saName),
		},
		Data: map[string][]byte{
			"cert": cert,
			"key":  key,
		},
	}

	// TODO: We currently use Update() instead of Create() to handle pre-existing
	// secrets. We should however do it correctly by querying the apiserver to
	// check secret existence first.
	_, err := sc.core.Secrets(saNamespace).Update(secret)
	// Ingore `NotFound` error since it is returned when updating a non-existent
	// secret.
	if err != nil && !errors.IsNotFound(err) {
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
