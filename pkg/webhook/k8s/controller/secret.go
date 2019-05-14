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
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/webhook/model"
	"istio.io/istio/pkg/webhook/util"
	"istio.io/pkg/log"
)

/* #nosec: disable gas linter */
const (
	// The ID/name for the certificate chain file.
	CertChainID = "cert-chain.pem"
	// The ID/name for the private key file.
	PrivateKeyID = "key.pem"
	// The ID/name for the CA root certificate file.
	RootCertID = "root-cert.pem"

	secretNamePrefix   = "istio."
	secretResyncPeriod = time.Minute

	// DefaultWorkloadCertGracePeriodRatio is the default length of certificate rotation grace period,
	// configured as the ratio of the certificate TTL.
	DefaultWorkloadCertGracePeriodRatio = 0.2

	// The number of retries when requesting to create secret.
	secretCreationRetry = 3
)

// SecretController manages the specificed secret that contains keys and certificates.
type SecretController struct {
	args    *model.Args
	certTTL time.Duration
	core    corev1.CoreV1Interface
	// Length of the grace period for the certificate rotation.
	gracePeriodRatio float32

	// Controller and store for secret objects.
	scrtController cache.Controller
	scrtStore      cache.Store

	// service name for certificate
	svcName string
	// namespace for this secret
	namespace string
}

// NewSecretController returns a pointer to a newly constructed SecretController instance.
func NewSecretController(caArgs *model.Args, core corev1.CoreV1Interface,
	svcName, namespace string) (*SecretController, error) {

	c := &SecretController{
		args:             caArgs,
		certTTL:          caArgs.RequestedCertTTL,
		gracePeriodRatio: DefaultWorkloadCertGracePeriodRatio,
		svcName:          svcName,
		core:             core,
		namespace:        namespace,
	}

	scrtLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = "app=" + c.svcName
			return core.Secrets(namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = "app=" + c.svcName
			return core.Secrets(namespace).Watch(options)
		},
	}
	c.scrtStore, c.scrtController =
		cache.NewInformer(scrtLW, &v1.Secret{}, secretResyncPeriod, cache.ResourceEventHandlerFuncs{
			AddFunc:    c.scrtAdded,
			DeleteFunc: c.scrtDeleted,
			UpdateFunc: c.scrtUpdated,
		})

	return c, nil
}

// Run starts the SecretController until a value is sent to stopCh.
func (sc *SecretController) Run(stopCh chan struct{}) {
	go sc.scrtController.Run(stopCh)
	//create the self signed certificate for webhook
	sc.UpsertSecret()
}

// UpsertSecret upserts the self signed certificate and key
func (sc *SecretController) UpsertSecret() {

	secret := sc.buildSecret()

	_, exists, err := sc.scrtStore.Get(secret)
	if err != nil {
		log.Errorf("Failed to get secret from the store (error %v)", err)
	}

	if exists {
		// Do nothing for existing secrets. Rotating expiring certs are handled by the `scrtUpdated` method.
		return
	}

	// Now we know the secret does not exist yet. So we create a new one.
	rootCert, chain, key, err := util.GenerateKeyAndCert(sc.args, sc.svcName, sc.namespace)
	if err != nil {
		log.Errorf("Failed to generate key and certificate for secret %q in namespace %q (error %v)",
			GetSecretName(sc.svcName), sc.namespace, err)

		return
	}
	secret.Data = map[string][]byte{
		CertChainID:  chain,
		PrivateKeyID: key,
		RootCertID:   rootCert,
	}

	// We retry several times when create secret to mitigate transient network failures.
	for i := 0; i < secretCreationRetry; i++ {
		_, err = sc.core.Secrets(sc.namespace).Create(secret)
		if err == nil || errors.IsAlreadyExists(err) {
			if errors.IsAlreadyExists(err) {
				log.Infof("Secret \"%s\" in namespace \"%s\" already exists", GetSecretName(sc.svcName), sc.namespace)
			}
			break
		} else {
			log.Errorf("Failed to create secret in attempt %v/%v, (error: %s)", i+1, secretCreationRetry, err)
		}
		time.Sleep(time.Second)
	}

	if err != nil && !errors.IsAlreadyExists(err) {
		log.Errorf("Failed to create secret \"%s\"  (error: %s), retries %v times",
			GetSecretName(sc.svcName), err, secretCreationRetry)
		return
	}

	log.Infof("Secret \"%s\" in namespace \"%s\" has been created", GetSecretName(sc.svcName), sc.namespace)
}

func (sc *SecretController) scrtDeleted(obj interface{}) {
	scrt, ok := obj.(*v1.Secret)
	if !ok {
		log.Warnf("Failed to convert to secret object: %v", obj)
		return
	}

	log.Infof("Re-create deleted secret %s", scrt.GetName())
	sc.UpsertSecret()
}

func (sc *SecretController) scrtAdded(obj interface{}) {
	scrt, ok := obj.(*v1.Secret)
	if !ok {
		log.Warnf("Failed to convert to secret object: %v", obj)
		return
	}
	// Create certificate and key files to be used by webhook
	err := util.CreateCAFiles(sc.args, scrt.Data[CertChainID], scrt.Data[PrivateKeyID], scrt.Data[RootCertID])
	if err != nil {
		log.Errorf("Failed to create file: %v", err)
		return
	}
}

func (sc *SecretController) scrtUpdated(oldObj, newObj interface{}) {

	scrt, ok := newObj.(*v1.Secret)
	if !ok {
		log.Warnf("Failed to convert to secret object: %v", newObj)
		return
	}
	namespace := scrt.GetNamespace()
	name := scrt.GetName()

	certBytes := scrt.Data[CertChainID]
	cert, err := util.ParsePemEncodedCertificate(certBytes)
	if err != nil {
		log.Warnf("Failed to parse certificates in secret %s/%s (error: %v), refreshing the secret.",
			namespace, name, err)
		if err = sc.refreshSecret(scrt); err != nil {
			log.Errora(err)
		}

		return
	}

	certLifeTimeLeft := time.Until(cert.NotAfter)
	certLifeTime := cert.NotAfter.Sub(cert.NotBefore)

	// Because time.Duration only takes int type, multiply gracePeriodRatio by 1000 and then divide it.
	gracePeriod := time.Duration(sc.gracePeriodRatio*1000) * certLifeTime / 1000

	// Refresh the secret if the certificate contained in the secret is about to expire
	if certLifeTimeLeft < gracePeriod {
		log.Infof("Refreshing secret %s/%s, either the leaf certificate is about to expire "+
			"or the root certificate is outdated", namespace, name)

		if err = sc.refreshSecret(scrt); err != nil {
			log.Errorf("Failed to update secret %s/%s (error: %s)", namespace, name, err)
		}
	}
}

// refreshSecret is an inner func to refresh cert secrets when necessary
func (sc *SecretController) refreshSecret(scrt *v1.Secret) error {
	namespace := scrt.GetNamespace()

	rootCert, chain, key, err := util.GenerateKeyAndCert(sc.args, sc.svcName, namespace)
	if err != nil {
		return err
	}

	scrt.Data[CertChainID] = chain
	scrt.Data[PrivateKeyID] = key
	scrt.Data[RootCertID] = rootCert

	_, err = sc.core.Secrets(namespace).Update(scrt)
	return err
}

// GetSecretName returns the secret name.
func GetSecretName(svcName string) string {
	return secretNamePrefix + svcName
}

func (sc *SecretController) buildSecret() *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSecretName(sc.svcName),
			Namespace: sc.namespace,
			Labels: map[string]string{
				"app": sc.svcName,
			},
		},
		Type: v1.SecretTypeOpaque,
	}
}
