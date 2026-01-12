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
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
)

const (
	// The ID/name for the certificate chain in kubernetes generic secret.
	GenericScrtCert = "cert"
	// The ID/name for the private key in kubernetes generic secret.
	GenericScrtKey = "key"
	// The ID/name for the CA certificate in kubernetes generic secret.
	GenericScrtCaCert = "cacert"
	// The ID/name for the CRL in kubernetes generic secret.
	GenericScrtCRL = "crl"

	// The ID/name for the certificate chain in kubernetes tls secret.
	TLSSecretCert = "tls.crt"
	// The ID/name for the k8sKey in kubernetes tls secret.
	TLSSecretKey = "tls.key"
	// The ID/name for the certificate OCSP staple in kubernetes tls secret
	TLSSecretOcspStaple = "tls.ocsp-staple"
	// The ID/name for the CA certificate in kubernetes tls secret
	TLSSecretCaCert = "ca.crt"
	// The ID/name for the CRL in kubernetes tls secret.
	TLSSecretCrl = "ca.crl"
)

type CredentialsController struct {
	secrets         kclient.Client[*v1.Secret]
	configMaps      kclient.Client[*v1.ConfigMap]
	sar             authorizationv1client.SubjectAccessReviewInterface
	isConfigCluster bool

	mu                 sync.RWMutex
	authorizationCache map[authorizationKey]authorizationResponse
}

type authorizationKey string

type authorizationResponse struct {
	expiration time.Time
	authorized error
}

var _ credentials.Controller = &CredentialsController{}

// SecretsFieldSelector is an optimization to avoid excessive secret bloat.
// We only care about TLS certificates and docker config for Wasm image pulling.
// Unfortunately, it is not as simple as selecting type=kubernetes.io/tls and type=kubernetes.io/dockerconfigjson.
// Because of legacy reasons and supporting an extra ca.crt, we also support generic types.
// Its also likely users have started to use random types and expect them to continue working.
// This makes the assumption we will never care about Helm secrets or SA token secrets - two common
// large secrets in clusters.
// This is a best effort optimization only; the code would behave correctly if we watched all secrets.
var SecretsFieldSelector = fields.AndSelectors(
	fields.OneTermNotEqualSelector("type", "helm.sh/release.v1"),
	fields.OneTermNotEqualSelector("type", string(v1.SecretTypeServiceAccountToken))).String()

func NewCredentialsController(kc kube.Client, handlers []func(typ kind.Kind, name string, namespace string), isConfigCluster bool) *CredentialsController {
	secrets := kclient.NewFiltered[*v1.Secret](kc, kclient.Filter{
		FieldSelector: SecretsFieldSelector,
		ObjectFilter:  kc.ObjectFilter(),
	})

	var configMaps kclient.Client[*v1.ConfigMap]
	if isConfigCluster {
		configMaps = kclient.NewFiltered[*v1.ConfigMap](kc, kclient.Filter{
			ObjectFilter: kc.ObjectFilter(),
		})
	}

	for _, h := range handlers {
		// register handler before informer starts
		secrets.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
			h(kind.Secret, o.GetName(), o.GetNamespace())
		}))
		if configMaps != nil {
			configMaps.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
				_, err := ExtractRootFromString(o.(*v1.ConfigMap).Data)
				if err == nil {
					// Only trigger updates for ConfigMaps that are actually possibly used
					h(kind.ConfigMap, o.GetName(), o.GetNamespace())
				}
			}))
		}
	}

	return &CredentialsController{
		secrets:            secrets,
		configMaps:         configMaps,
		sar:                kc.Kube().AuthorizationV1().SubjectAccessReviews(),
		isConfigCluster:    isConfigCluster,
		authorizationCache: make(map[authorizationKey]authorizationResponse),
	}
}

func (s *CredentialsController) Close() {
	s.secrets.ShutdownHandlers()
	if s.configMaps != nil {
		s.configMaps.ShutdownHandlers()
	}
}

func (s *CredentialsController) HasSynced() bool {
	synced := s.secrets.HasSynced()
	if s.configMaps != nil {
		synced = synced && s.configMaps.HasSynced()
	}
	return synced
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

func (s *CredentialsController) GetCertInfo(name, namespace string) (certInfo *credentials.CertInfo, err error) {
	k8sSecret := s.secrets.Get(name, namespace)
	if k8sSecret == nil {
		return nil, fmt.Errorf("secret %v/%v not found", namespace, name)
	}

	return ExtractCertInfo(k8sSecret)
}

func (s *CredentialsController) GetCaCert(name, namespace string) (certInfo *credentials.CertInfo, err error) {
	k8sSecret := s.secrets.Get(name, namespace)
	if k8sSecret == nil {
		strippedName := strings.TrimSuffix(name, securitymodel.SdsCaSuffix)
		// Could not fetch cert, look for secret without -cacert suffix
		k8sSecret := s.secrets.Get(strippedName, namespace)
		if k8sSecret == nil {
			return nil, fmt.Errorf("secret %v/%v not found", namespace, strippedName)
		}
		return ExtractRoot(k8sSecret.Data)
	}
	return ExtractRoot(k8sSecret.Data)
}

func (s *CredentialsController) GetConfigMapCaCert(name, namespace string) (certInfo *credentials.CertInfo, err error) {
	if !s.isConfigCluster {
		return nil, fmt.Errorf("configmap access not enabled for remote clusters")
	}
	strippedName := strings.TrimSuffix(name, securitymodel.SdsCaSuffix)
	cm := s.configMaps.Get(strippedName, namespace)
	if cm == nil {
		return nil, fmt.Errorf("configmap %v/%v not found", namespace, strippedName)
	}

	return ExtractRootFromString(cm.Data)
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

// ExtractCertInfo extracts server key, certificate, and OCSP staple
func ExtractCertInfo(scrt *v1.Secret) (certInfo *credentials.CertInfo, err error) {
	ret := &credentials.CertInfo{}
	if hasValue(scrt.Data, GenericScrtCert, GenericScrtKey) {
		ret.Cert = scrt.Data[GenericScrtCert]
		ret.Key = scrt.Data[GenericScrtKey]
		ret.CRL = scrt.Data[GenericScrtCRL]
		return ret, nil
	}
	if hasValue(scrt.Data, TLSSecretCert, TLSSecretKey) {
		ret.Cert = scrt.Data[TLSSecretCert]
		ret.Key = scrt.Data[TLSSecretKey]
		ret.Staple = scrt.Data[TLSSecretOcspStaple]
		ret.CRL = scrt.Data[TLSSecretCrl]
		return ret, nil
	}
	// No cert found. Try to generate a helpful error message
	if hasKeys(scrt.Data, GenericScrtCert, GenericScrtKey) {
		return nil, fmt.Errorf("found keys %q and %q, but they were empty", GenericScrtCert, GenericScrtKey)
	}
	if hasKeys(scrt.Data, TLSSecretCert, TLSSecretKey) {
		return nil, fmt.Errorf("found keys %q and %q, but they were empty", TLSSecretCert, TLSSecretKey)
	}
	found := truncatedKeysMessage(scrt.Data)
	return nil, fmt.Errorf("found secret, but didn't have expected keys (%s and %s) or (%s and %s); found: %s",
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

// ExtractRootFromString extracts the root certificate
func ExtractRootFromString(data map[string]string) (certInfo *credentials.CertInfo, err error) {
	conv := make(map[string][]byte, len(data))
	for k, v := range data {
		conv[k] = []byte(v)
	}
	return ExtractRoot(conv)
}

// ExtractRoot extracts the root certificate
func ExtractRoot(data map[string][]byte) (certInfo *credentials.CertInfo, err error) {
	ret := &credentials.CertInfo{}
	if hasValue(data, GenericScrtCaCert) {
		ret.Cert = data[GenericScrtCaCert]
		ret.CRL = data[GenericScrtCRL]
		return ret, nil
	}
	if hasValue(data, TLSSecretCaCert) {
		ret.Cert = data[TLSSecretCaCert]
		ret.CRL = data[TLSSecretCrl]
		return ret, nil
	}
	// No cert found. Try to generate a helpful error message
	if hasKeys(data, GenericScrtCaCert) {
		return nil, fmt.Errorf("found key %q, but it was empty", GenericScrtCaCert)
	}
	if hasKeys(data, TLSSecretCaCert) {
		return nil, fmt.Errorf("found key %q, but it was empty", TLSSecretCaCert)
	}
	found := truncatedKeysMessage(data)
	return nil, fmt.Errorf("found secret, but didn't have expected keys %s or %s; found: %s",
		GenericScrtCaCert, TLSSecretCaCert, found)
}
