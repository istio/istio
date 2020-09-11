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

package kube

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	informersv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/secrets"
	"istio.io/pkg/log"
)

const (
	// The ID/name for the certificate chain in kubernetes generic secret.
	GenericScrtCert = "cert"
	// The ID/name for the private key in kubernetes generic secret.
	GenericScrtKey = "key"
	// The ID/name for the CA certificate in kubernetes generic secret.
	GenericScrtCaCert = "cacert"

	// The ID/name for the certificate chain in kubernetes tls secret.
	TLSSecretCert = "tls.crt"
	// The ID/name for the k8sKey in kubernetes tls secret.
	TLSSecretKey = "tls.key"
	// The ID/name for the CA certificate in kubernetes tls secret
	TLSSecretCaCert = "ca.crt"

	// GatewaySdsCaSuffix is the suffix of the sds resource name for root CA. All resource
	// names for gateway root certs end with "-cacert".
	GatewaySdsCaSuffix = "-cacert"
)

type SecretsController struct {
	secrets informersv1.SecretInformer
}

var _ secrets.Controller = &SecretsController{}

func NewSecretsController(informer informersv1.SecretInformer) *SecretsController {
	// Informer is lazy loaded, load it now
	_ = informer.Informer()
	return &SecretsController{informer}
}

func (s *SecretsController) GetKeyAndCert(name, namespace string) (key []byte, cert []byte) {
	k8sSecret, err := s.secrets.Lister().Secrets(namespace).Get(name)
	if err != nil {
		return nil, nil
	}

	return extractKeyAndCert(k8sSecret)
}

func (s *SecretsController) GetCaCert(name, namespace string) (cert []byte) {
	strippedName := strings.TrimSuffix(name, GatewaySdsCaSuffix)
	k8sSecret, err := s.secrets.Lister().Secrets(namespace).Get(strippedName)
	var rootCert []byte
	if err != nil {
		// Could not fetch cert, look for legacy secret with -cacert suffix
		k8sSecret, caCertErr := s.secrets.Lister().Secrets(namespace).Get(name)
		if caCertErr != nil {
			return nil
		}
		rootCert = extractRoot(k8sSecret)
	} else {
		rootCert = extractRoot(k8sSecret)
		// Secret exists, but does not have the ca cert. Fall back to -cacert secret
		if rootCert == nil {
			k8sSecret, caCertErr := s.secrets.Lister().Secrets(namespace).Get(name)
			if caCertErr != nil {
				return nil
			}
			rootCert = extractRoot(k8sSecret)
		}
	}
	return rootCert
}

// extractKeyAndCert extracts server key, certificate
func extractKeyAndCert(scrt *v1.Secret) (key, cert []byte) {
	if len(scrt.Data[GenericScrtCert]) > 0 {
		cert = scrt.Data[GenericScrtCert]
		key = scrt.Data[GenericScrtKey]
	} else {
		cert = scrt.Data[TLSSecretCert]
		key = scrt.Data[TLSSecretKey]
	}
	return key, cert
}

// extractRoot extracts the root certificate
func extractRoot(scrt *v1.Secret) (cert []byte) {
	if len(scrt.Data[GenericScrtCaCert]) > 0 {
		return scrt.Data[GenericScrtCaCert]
	} else if len(scrt.Data[TLSSecretCaCert]) > 0 {
		return scrt.Data[TLSSecretCaCert]
	}
	return nil
}

func (s *SecretsController) AddEventHandler(f func(name string, namespace string)) {
	handler := func(obj interface{}) {
		scrt, ok := obj.(*v1.Secret)
		if !ok {
			if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				if cast, ok := tombstone.Obj.(*v1.Secret); ok {
					scrt = cast
				} else {
					log.Errorf("Failed to convert to tombstoned secret object: %v", obj)
					return
				}
			} else {
				log.Errorf("Failed to convert to secret object: %v", obj)
				return
			}
		}
		f(scrt.Name, scrt.Namespace)
	}
	s.secrets.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				handler(obj)
			},
			UpdateFunc: func(old, cur interface{}) {
				handler(cur)
			},
			DeleteFunc: func(obj interface{}) {
				handler(obj)
			},
		})
}
