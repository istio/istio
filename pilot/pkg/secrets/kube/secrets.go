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
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informersv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	authorizationv1client "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/secrets"
	"istio.io/istio/pkg/kube"
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
	sar     authorizationv1client.SubjectAccessReviewInterface

	clusterID    string
	remoteGetter RemoteKubeClientGetter

	mu                   sync.RWMutex
	authorizationCache   map[authorizationKey]authorizationResponse
	authorizationEnabled bool
}

type authorizationKey struct {
	cluster string
	user    string
}

type authorizationResponse struct {
	expiration time.Time
	authorized error
}

var _ secrets.Controller = &SecretsController{}

type RemoteKubeClientGetter func(clusterID string) kubernetes.Interface

func NewSecretsController(client kube.Client, clusterID string, remoteClientGetter RemoteKubeClientGetter) *SecretsController {
	// Informer is lazy loaded, load it now
	_ = client.KubeInformer().Core().V1().Secrets().Informer()

	return &SecretsController{
		secrets: client.KubeInformer().Core().V1().Secrets(),

		authorizationEnabled: true,
		sar:                  client.AuthorizationV1().SubjectAccessReviews(),
		clusterID:            clusterID,
		remoteGetter:         remoteClientGetter,
		authorizationCache:   make(map[authorizationKey]authorizationResponse),
	}
}

func toUser(serviceAccount, namespace string) string {
	return fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccount)
}

const cacheTTL = time.Minute

// clearExpiredCache iterates through the cache and removes all expired entries. Should be called with mutex held.
func (s *SecretsController) clearExpiredCache() {
	for k, v := range s.authorizationCache {
		if v.expiration.Before(time.Now()) {
			delete(s.authorizationCache, k)
		}
	}
}

// cachedAuthorization checks the authorization cache
// nolint
func (s *SecretsController) cachedAuthorization(user, clusterID string) (error, bool) {
	key := authorizationKey{cluster: clusterID, user: user}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authorizationEnabled {
		return nil, true
	}
	s.clearExpiredCache()
	// No need to check expiration, we will evict expired entries above
	got, f := s.authorizationCache[key]
	if !f {
		return nil, false
	}
	return got.authorized, true
}

// cachedAuthorization checks the authorization cache
func (s *SecretsController) insertCache(user, clusterID string, response error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := authorizationKey{cluster: clusterID, user: user}
	expDelta := cacheTTL
	if response == nil {
		// Cache success a bit longer, there is no need to quickly revoke access
		expDelta *= 5
	}
	log.Debugf("cached authorization for user %s: %v", user, response)
	s.authorizationCache[key] = authorizationResponse{
		expiration: time.Now().Add(expDelta),
		authorized: response,
	}
}

func (s *SecretsController) getAccessReviewClient(clusterID string) authorizationv1client.SubjectAccessReviewInterface {
	// first match local/primary cluster
	if s.clusterID == clusterID || clusterID == "" {
		return s.sar
	}

	// secondly try other remote clusters
	if s.remoteGetter != nil {
		if res := s.remoteGetter(clusterID); res != nil {
			return res.AuthorizationV1().SubjectAccessReviews()
		}
	}

	return nil
}

// DisableAuthorization makes the authorization check always pass. Should be used only for tests.
func (s *SecretsController) DisableAuthorization() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorizationEnabled = false
}

func (s *SecretsController) Authorize(serviceAccount, namespace, clusterID string) error {
	user := toUser(serviceAccount, namespace)
	if cached, f := s.cachedAuthorization(user, clusterID); f {
		return cached
	}
	resp := func() error {
		resp, err := s.sar.Create(context.Background(), &authorizationv1.SubjectAccessReview{
			ObjectMeta: metav1.ObjectMeta{},
			Spec: authorizationv1.SubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Verb:      "list",
					Resource:  "secrets",
				},
				User: user,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		if !resp.Status.Allowed {
			return fmt.Errorf("%s/%s is not authorized to read secrets: %v", serviceAccount, namespace, resp.Status.Reason)
		}
		return nil
	}()
	s.insertCache(user, clusterID, resp)
	return resp
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
