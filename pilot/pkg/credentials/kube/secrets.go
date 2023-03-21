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
	"sort"
	"strings"
	"sync"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	sa "k8s.io/apiserver/pkg/authentication/serviceaccount"
	authorizationv1client "k8s.io/client-go/kubernetes/typed/authorization/v1"

	"istio.io/istio/pilot/pkg/credentials"
	securitymodel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
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
	// The ID/name for the certificate OCSP staple in kubernetes tls secret
	TLSSecretOcspStaple = "tls.ocsp-staple"
	// The ID/name for the CA certificate in kubernetes tls secret
	TLSSecretCaCert = "ca.crt"
)

type CredentialsController struct {
	secrets kclient.Client[*v1.Secret]
	sar     authorizationv1client.SubjectAccessReviewInterface

	mu                 sync.RWMutex
	authorizationCache map[authorizationKey]authorizationResponse
}

type authorizationKey string

type authorizationResponse struct {
	expiration time.Time
	authorized error
}

var _ credentials.Controller = &CredentialsController{}

func NewCredentialsController(kc kube.Client) *CredentialsController {
	// We only care about TLS certificates and docker config for Wasm image pulling.
	// Unfortunately, it is not as simple as selecting type=kubernetes.io/tls and type=kubernetes.io/dockerconfigjson.
	// Because of legacy reasons and supporting an extra ca.crt, we also support generic types.
	// Its also likely users have started to use random types and expect them to continue working.
	// This makes the assumption we will never care about Helm secrets or SA token secrets - two common
	// large secrets in clusters.
	// This is a best effort optimization only; the code would behave correctly if we watched all secrets.
	fieldSelector := fields.AndSelectors(
		fields.OneTermNotEqualSelector("type", "helm.sh/release.v1"),
		fields.OneTermNotEqualSelector("type", string(v1.SecretTypeServiceAccountToken))).String()
	secrets := kclient.NewFiltered[*v1.Secret](kc, kclient.Filter{
		FieldSelector: fieldSelector,
	})

	return &CredentialsController{
		secrets:            secrets,
		sar:                kc.Kube().AuthorizationV1().SubjectAccessReviews(),
		authorizationCache: make(map[authorizationKey]authorizationResponse),
	}
}

const cacheTTL = time.Minute

// clearExpiredCache iterates through the cache and removes all expired entries. Should be called with mutex held.
func (s *CredentialsController) clearExpiredCache() {
	for k, v := range s.authorizationCache {
		if v.expiration.Before(time.Now()) {
			delete(s.authorizationCache, k)
		}
	}
}

// cachedAuthorization checks the authorization cache
// nolint
func (s *CredentialsController) cachedAuthorization(user string) (error, bool) {
	key := authorizationKey(user)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearExpiredCache()
	// No need to check expiration, we will evict expired entries above
	got, f := s.authorizationCache[key]
	if !f {
		return nil, false
	}
	return got.authorized, true
}

// cachedAuthorization checks the authorization cache
func (s *CredentialsController) insertCache(user string, response error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := authorizationKey(user)
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

func (s *CredentialsController) Authorize(serviceAccount, namespace string) error {
	user := sa.MakeUsername(namespace, serviceAccount)
	if cached, f := s.cachedAuthorization(user); f {
		return cached
	}
	err := func() error {
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
	s.insertCache(user, err)
	return err
}

func (s *CredentialsController) GetKeyCertAndStaple(name, namespace string) (key []byte, cert []byte, staple []byte, err error) {
	k8sSecret := s.secrets.Get(name, namespace)
	if k8sSecret == nil {
		return nil, nil, nil, fmt.Errorf("secret %v/%v not found", namespace, name)
	}

	return ExtractKeyCertAndStaple(k8sSecret)
}

func (s *CredentialsController) GetCaCert(name, namespace string) (cert []byte, err error) {
	k8sSecret := s.secrets.Get(name, namespace)
	if k8sSecret == nil {
		strippedName := strings.TrimSuffix(name, securitymodel.SdsCaSuffix)
		// Could not fetch cert, look for secret without -cacert suffix
		k8sSecret := s.secrets.Get(strippedName, namespace)
		if k8sSecret == nil {
			return nil, fmt.Errorf("secret %v/%v not found", namespace, strippedName)
		}
		return extractRoot(k8sSecret)
	}
	return extractRoot(k8sSecret)
}

func (s *CredentialsController) GetDockerCredential(name, namespace string) ([]byte, error) {
	k8sSecret := s.secrets.Get(name, namespace)
	if k8sSecret == nil {
		return nil, fmt.Errorf("secret %v/%v not found", namespace, name)
	}
	if k8sSecret.Type != v1.SecretTypeDockerConfigJson {
		return nil, fmt.Errorf("type of secret %v/%v is not %v", namespace, name, v1.SecretTypeDockerConfigJson)
	}
	if cred, found := k8sSecret.Data[v1.DockerConfigJsonKey]; found {
		return cred, nil
	}
	return nil, fmt.Errorf("cannot find docker config at secret %v/%v", namespace, name)
}

func hasKeys(d map[string][]byte, keys ...string) bool {
	for _, k := range keys {
		_, f := d[k]
		if !f {
			return false
		}
	}
	return true
}

func hasValue(d map[string][]byte, keys ...string) bool {
	for _, k := range keys {
		v := d[k]
		if len(v) == 0 {
			return false
		}
	}
	return true
}

// ExtractKeyCertAndStaple extracts server key, certificate and OCSP staple
func ExtractKeyCertAndStaple(scrt *v1.Secret) (key, cert, staple []byte, err error) {
	if hasValue(scrt.Data, GenericScrtCert, GenericScrtKey) {
		return scrt.Data[GenericScrtKey], scrt.Data[GenericScrtCert], nil, nil
	}
	if hasValue(scrt.Data, TLSSecretCert, TLSSecretKey, TLSSecretOcspStaple) {
		return scrt.Data[TLSSecretKey], scrt.Data[TLSSecretCert], scrt.Data[TLSSecretOcspStaple], nil
	}
	if hasValue(scrt.Data, TLSSecretCert, TLSSecretKey) {
		return scrt.Data[TLSSecretKey], scrt.Data[TLSSecretCert], nil, nil
	}
	// No cert found. Try to generate a helpful error messsage
	if hasKeys(scrt.Data, GenericScrtCert, GenericScrtKey) {
		return nil, nil, nil, fmt.Errorf("found keys %q and %q, but they were empty", GenericScrtCert, GenericScrtKey)
	}
	if hasKeys(scrt.Data, TLSSecretCert, TLSSecretKey) {
		return nil, nil, nil, fmt.Errorf("found keys %q and %q, but they were empty", TLSSecretCert, TLSSecretKey)
	}
	found := truncatedKeysMessage(scrt.Data)
	return nil, nil, nil, fmt.Errorf("found secret, but didn't have expected keys (%s and %s) or (%s and %s); found: %s",
		GenericScrtCert, GenericScrtKey, TLSSecretCert, TLSSecretKey, found)
}

func truncatedKeysMessage(data map[string][]byte) string {
	keys := []string{}
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) < 3 {
		return strings.Join(keys, ", ")
	}
	return fmt.Sprintf("%s, and %d more...", strings.Join(keys[:3], ", "), len(keys)-3)
}

// extractRoot extracts the root certificate
func extractRoot(scrt *v1.Secret) (cert []byte, err error) {
	if hasValue(scrt.Data, GenericScrtCaCert) {
		return scrt.Data[GenericScrtCaCert], nil
	}
	if hasValue(scrt.Data, TLSSecretCaCert) {
		return scrt.Data[TLSSecretCaCert], nil
	}
	// No cert found. Try to generate a helpful error messsage
	if hasKeys(scrt.Data, GenericScrtCaCert) {
		return nil, fmt.Errorf("found key %q, but it was empty", GenericScrtCaCert)
	}
	if hasKeys(scrt.Data, TLSSecretCaCert) {
		return nil, fmt.Errorf("found key %q, but it was empty", TLSSecretCaCert)
	}
	found := truncatedKeysMessage(scrt.Data)
	return nil, fmt.Errorf("found secret, but didn't have expected keys %s or %s; found: %s",
		GenericScrtCaCert, TLSSecretCaCert, found)
}

func (s *CredentialsController) AddEventHandler(h func(name string, namespace string)) {
	// register handler before informer starts
	s.secrets.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		h(o.GetName(), o.GetNamespace())
	}))
}
