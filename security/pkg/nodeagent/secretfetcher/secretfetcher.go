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

package secretfetcher

import (
	"fmt"
	"os"
	"sync"
	"time"

	"istio.io/istio/pkg/log"

	ca "istio.io/istio/security/pkg/nodeagent/caclient"

	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"

	"istio.io/istio/security/pkg/nodeagent/model"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	secretResyncPeriod = time.Minute

	IngressSecretType = "istio.io/ingress-key-cert"

	IngressSecretNameSpace = "istio-ingress"

	KubeConfigFile = "kube-config"

	// The prefix of a key which maps to a kubernetes secret. The resource name in a SDS request sent
	// by an ingress gateway is secret.<secret name>. We use the same format as key for secret lookup.
	resourcePrefix = "secret."
	// The ID/name for the certificate chain in kubernetes secret.
	scrtCert = "cert"
	// The ID/name for the key in kubernetes secret.
	scrtKey = "key"
)

// SecretFetcher fetches secret via watching k8s secrets or sending CSR to CA.
type SecretFetcher struct {
	// If UseCaClient is true, use caClient to send CSR to CA.
	UseCaClient bool
	CaClient    caClientInterface.Client

	// Controller and store for secret objects.
	scrtController cache.Controller
	scrtStore      cache.Store

	// secrets maps key to secrets
	secrets sync.Map
}

func fatalf(template string, args ...interface{}) {
	if len(args) > 0 {
		log.Errorf(template, args...)
	} else {
		log.Errorf(template)
	}
	os.Exit(-1)
}

// createClientset creates kubernetes client to watch kubernetes secrets.
func createClientset() *kubernetes.Clientset {
	c, err := clientcmd.BuildConfigFromFlags("", KubeConfigFile)
	if err != nil {
		fatalf("Failed to create a config for kubernetes client (error: %s)", err)
	}
	cs, err := kubernetes.NewForConfig(c)
	if err != nil {
		fatalf("Failed to create a clientset (error: %s)", err)
	}
	return cs
}

func NewSecretFetcher(ingressGatewayAgent bool, endpoint, CAProviderName string, tlsFlag bool) (*SecretFetcher, error) {
	ret := &SecretFetcher{}

	if ingressGatewayAgent {
		ret.UseCaClient = false
		istioSecretSelector := fields.SelectorFromSet(map[string]string{"type": IngressSecretType}).String()
		cs := createClientset()
		scrtLW := &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.FieldSelector = istioSecretSelector
				return cs.CoreV1().Secrets(IngressSecretNameSpace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.FieldSelector = istioSecretSelector
				return cs.CoreV1().Secrets(IngressSecretNameSpace).Watch(options)
			},
		}
		ret.scrtStore, ret.scrtController =
			cache.NewInformer(scrtLW, &v1.Secret{}, secretResyncPeriod, cache.ResourceEventHandlerFuncs{
				AddFunc:    ret.scrtAdded,
				DeleteFunc: ret.scrtDeleted,
				// TODO(jimmycyj): add handler for UpdateFunc.
			})
	} else {
		caClient, err := ca.NewCAClient(endpoint, CAProviderName, tlsFlag)
		if err != nil {
			log.Errorf("failed to create caClient: %v", err)
			return ret, fmt.Errorf("failed to create caClient")
		}
		ret.UseCaClient = true
		ret.CaClient = caClient
	}

	return ret, nil
}

func (sc *SecretFetcher) scrtAdded(obj interface{}) {
	scrt, ok := obj.(*v1.Secret)
	if !ok {
		log.Warnf("Failed to convert to secret object: %v", obj)
		return
	}

	t := time.Now()
	resourceName := resourcePrefix + scrt.GetName()

	ns := &model.SecretItem{
		ResourceName:     resourceName,
		CertificateChain: scrt.Data[scrtCert],
		PrivateKey:       scrt.Data[scrtKey],
		CreatedTime:      t,
		Version:          t.String(),
	}
	sc.secrets.Store(resourceName, *ns)
	log.Debugf("secret %s is added", scrt.GetName())
}

func (sc *SecretFetcher) scrtDeleted(obj interface{}) {
	scrt, ok := obj.(*v1.Secret)
	if !ok {
		log.Warnf("Failed to convert to secret object: %v", obj)
		return
	}

	key := resourcePrefix + scrt.GetName()
	sc.secrets.Delete(key)
	log.Debugf("secret %s is deleted", scrt.GetName())
}

// FindIngressGatewaySecret returns the secret for a key, or empty secret if no
// secret is present. The ok result indicates whether secret was found.
func (sc *SecretFetcher) FindIngressGatewaySecret(key string) (secret model.SecretItem, ok bool) {
	val, exist := sc.secrets.Load(key)
	if !exist {
		return model.SecretItem{}, false
	}
	e := val.(model.SecretItem)
	return e, true
}
