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
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/spiffe"
	k8ssecret "istio.io/istio/security/pkg/k8s/secret"
	"istio.io/istio/security/pkg/listwatch"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	certutil "istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
)

/* #nosec: disable gas linter */
const (
	// The Istio secret annotation type
	IstioSecretType = "istio.io/key-and-cert"

	// NamespaceManagedLabel (string, with namespace as value) and NamespaceOverrideLabel (boolean) contribute to determining
	// whether or not a given Citadel instance should operate on a namespace. The behavior is as follows:
	// 1) If NamespaceOverrideLabel exists and is valid, follow what this label tells us
	// 2) If not, check NamespaceManagedLabel. If the value matches the Citadel instance's NS, then it should be active
	// 3) If NamespaceManagedLabel nonexistent or invalid, follow enableNamespacesByDefault, set from "CITADEL_ENABLE_NAMESPACES_BY_DEFAULT" envvar
	// 4) If enableNamespacesByDefault is "true", the Citadel instance should operate on unlabeled namespaces, otherwise should not
	NamespaceManagedLabel  = "ca.istio.io/env"
	NamespaceOverrideLabel = "ca.istio.io/override"

	// The ID/name for the certificate chain file.
	CertChainID = "cert-chain.pem"
	// The ID/name for the private key file.
	PrivateKeyID = "key.pem"
	// The ID/name for the CA root certificate file.
	RootCertID = "root-cert.pem"
	// The key to specify corresponding service account in the annotation of K8s secrets.
	ServiceAccountNameAnnotationKey = "istio.io/service-account.name"
	// ConfigMap label key that indicates that the ConfigMap contains additional trusted CA roots.
	ExtraTrustAnchorsLabel = "istio.io/extra-trust-anchors"

	secretNamePrefix      = "istio."
	secretResyncPeriod    = time.Minute
	namespaceResyncPeriod = time.Second * 5

	recommendedMinGracePeriodRatio = 0.2
	recommendedMaxGracePeriodRatio = 0.8

	// The size of a private key for a leaf certificate.
	keySize = 2048

	// The number of retries when requesting to create secret.
	secretCreationRetry = 3
)

var k8sControllerLog = log.RegisterScope("k8sController", "Citadel kubernetes controller log", 0)

// DNSNameEntry stores the service name and namespace to construct the DNS id.
// Service accounts matching the ServiceName and Namespace will have additional DNS SANs:
// ServiceName.Namespace.svc, ServiceName.Namespace and optional CustomDomain.
// This is intended for control plane and trusted services.
type DNSNameEntry struct {
	// ServiceName is the name of the service account to match
	ServiceName string

	// Namespace restricts to a specific namespace.
	Namespace string

	// CustomDomain allows adding a user-defined domain.
	CustomDomains []string
}

// certificateAuthority contains methods to be supported by a CA.
type certificateAuthority interface {
	// Sign generates a certificate for a workload or CA, from the given CSR and TTL.
	// TODO(myidpt): simplify this interface and pass a struct with cert field values instead.
	Sign(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error)
	// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain.
	SignWithCertChain(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error)
	// GetCAKeyCertBundle returns the KeyCertBundle used by CA.
	GetCAKeyCertBundle() util.KeyCertBundle
}

// SecretController manages the service accounts' secrets that contains Istio keys and certificates.
type SecretController struct {
	monitoring monitoringMetrics
	ca         certificateAuthority
	core       corev1.CoreV1Interface
	certUtil   certutil.CertUtil

	// Controller and store for service account objects.
	saController cache.Controller
	saStore      cache.Store

	// Controller and store for secret objects.
	scrtController cache.Controller
	scrtStore      cache.Store

	// Controller and store for namespace objects
	namespaceController cache.Controller
	namespaceStore      cache.Store

	// Used to coordinate with label and check if this instance of Citadel should create secret
	istioCaStorageNamespace string

	certTTL time.Duration

	minGracePeriod time.Duration

	// The set of namespaces explicitly set for monitoring via commandline (an entry could be metav1.NamespaceAll)
	namespaces map[string]struct{}

	// DNS-enabled serviceAccount.namespace to service pair
	dnsNames map[string]*DNSNameEntry

	// Length of the grace period for the certificate rotation.
	gracePeriodRatio float32

	// Whether controller loop should target namespaces without the NamespaceManagedLabel
	enableNamespacesByDefault bool

	// Whether the certificates are for dual-use clients (SAN+CN).
	dualUse bool

	// Whether the certificates are for CAs.
	forCA bool

	// If true, generate a PKCS#8 private key.
	pkcs8Key bool

	// Extra trust anchors related state
	extraTrustAnchorsController cache.Controller
	extraTrustAnchors           map[string]map[string]string // resource name -> field name -> field value
	primaryTrustAnchor          []byte
	combinedTrustAnchorsBundle  []byte
}

// NewSecretController returns a pointer to a newly constructed SecretController instance.
func NewSecretController(ca certificateAuthority, enableNamespacesByDefault bool, certTTL time.Duration,
	gracePeriodRatio float32, minGracePeriod time.Duration, dualUse bool,
	core corev1.CoreV1Interface, forCA bool, pkcs8Key bool, namespaces []string,
	dnsNames map[string]*DNSNameEntry, istioCaStorageNamespace string) (*SecretController, error) {

	if gracePeriodRatio < 0 || gracePeriodRatio > 1 {
		return nil, fmt.Errorf("grace period ratio %f should be within [0, 1]", gracePeriodRatio)
	}
	if gracePeriodRatio < recommendedMinGracePeriodRatio || gracePeriodRatio > recommendedMaxGracePeriodRatio {
		k8sControllerLog.Warnf("grace period ratio %f is out of the recommended window [%.2f, %.2f]",
			gracePeriodRatio, recommendedMinGracePeriodRatio, recommendedMaxGracePeriodRatio)
	}

	c := &SecretController{
		ca:                        ca,
		certTTL:                   certTTL,
		istioCaStorageNamespace:   istioCaStorageNamespace,
		gracePeriodRatio:          gracePeriodRatio,
		certUtil:                  certutil.NewCertUtil(int(gracePeriodRatio * 100)),
		enableNamespacesByDefault: enableNamespacesByDefault,
		minGracePeriod:            minGracePeriod,
		dualUse:                   dualUse,
		core:                      core,
		forCA:                     forCA,
		pkcs8Key:                  pkcs8Key,
		namespaces:                make(map[string]struct{}),
		dnsNames:                  dnsNames,
		monitoring:                newMonitoringMetrics(),
	}

	for _, ns := range namespaces {
		c.namespaces[ns] = struct{}{}
	}

	// listen for service account creation across all listened namespaces, filter out which should be ignored upon receipt
	saLW := listwatch.MultiNamespaceListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return core.ServiceAccounts(namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return core.ServiceAccounts(namespace).Watch(options)
			},
		}
	})
	c.saStore, c.saController =
		cache.NewInformer(saLW, &v1.ServiceAccount{}, time.Minute, cache.ResourceEventHandlerFuncs{
			AddFunc:    c.saAdded,
			DeleteFunc: c.saDeleted,
		})

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
	c.scrtStore, c.scrtController =
		cache.NewInformer(scrtLW, &v1.Secret{}, secretResyncPeriod, cache.ResourceEventHandlerFuncs{
			DeleteFunc: c.scrtDeleted,
			UpdateFunc: c.scrtUpdated,
		})

	namespaceLW := listwatch.MultiNamespaceListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return core.Namespaces().List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return core.Namespaces().Watch(options)
			}}
	})
	c.namespaceStore, c.namespaceController =
		cache.NewInformer(namespaceLW, &v1.Namespace{}, namespaceResyncPeriod, cache.ResourceEventHandlerFuncs{
			UpdateFunc: c.namespaceUpdated,
		})

	// Extra trust anchors related routines
	c.extraTrustAnchors = map[string]map[string]string{}
	c.refreshTrustAnchorsBundle()

	extraTrustAnchorsSelector := labels.SelectorFromSet(map[string]string{ExtraTrustAnchorsLabel: "true"}).String()
	extraTrustAnchorsLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = extraTrustAnchorsSelector
			return core.ConfigMaps(extraTrustAnchorsNamespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = extraTrustAnchorsSelector
			return core.ConfigMaps(extraTrustAnchorsNamespace).Watch(options)
		},
	}
	_, c.extraTrustAnchorsController = cache.NewInformer(extraTrustAnchorsLW, &v1.ConfigMap{}, time.Minute, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			log.Infof("Notified about a new ConfigMap containing extra trust anchors")
			newConfigMap := newObj.(*v1.ConfigMap)
			c.extraTrustAnchors[newConfigMap.GetName()] = newConfigMap.Data
			c.refreshTrustAnchorsBundle()
		},
		DeleteFunc: func(oldObj interface{}) {
			log.Infof("Notified about a deletion of an existing ConfigMap containing extra trust anchors")
			oldConfigMap := oldObj.(*v1.ConfigMap)
			delete(c.extraTrustAnchors, oldConfigMap.GetName())
			c.refreshTrustAnchorsBundle()
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			log.Infof("Notified about an update of an existing ConfigMap containing extra trust anchors")
			oldConfigMap := oldObj.(*v1.ConfigMap)
			newConfigMap := newObj.(*v1.ConfigMap)

			if !reflect.DeepEqual(oldConfigMap.Data, newConfigMap.Data) {
				c.extraTrustAnchors[newConfigMap.GetName()] = newConfigMap.Data
				c.refreshTrustAnchorsBundle()
			}
		},
	})

	return c, nil
}

// Run starts the SecretController until a value is sent to stopCh.
func (sc *SecretController) Run(stopCh chan struct{}) {
	go sc.extraTrustAnchorsController.Run(stopCh)
	cache.WaitForCacheSync(stopCh, sc.extraTrustAnchorsController.HasSynced)

	go sc.scrtController.Run(stopCh)

	// saAdded calls upsertSecret to update and insert secret
	// it throws error if the secret cache is not synchronized, but the secret exists in the system
	cache.WaitForCacheSync(stopCh, sc.scrtController.HasSynced)

	go sc.saController.Run(stopCh)
	go sc.namespaceController.Run(stopCh)
}

// GetSecretName returns the secret name for a given service account name.
func GetSecretName(saName string) string {
	return secretNamePrefix + saName
}

// Determine if the object is "enabled" for Citadel.
// Currently this looks at the list of watched namespaces and the object's namespace annotation
func (sc *SecretController) citadelManagedObject(obj metav1.Object) bool {

	// todo(incfly)
	// should be removed once listened namespaces flag phased out
	if _, watched := sc.namespaces[obj.GetNamespace()]; watched {
		return true
	}

	ns, err := sc.core.Namespaces().Get(obj.GetNamespace(), metav1.GetOptions{})
	if err != nil || ns == nil { // if we can't retrieve namespace details, fall back on default value
		k8sControllerLog.Errorf("could not retrieve namespace resource for object %s", obj.GetName())
		return sc.enableNamespacesByDefault
	}

	return sc.namespaceIsManaged(ns)
}

// Handles the event where a service account is added.
func (sc *SecretController) saAdded(obj interface{}) {
	acct := obj.(*v1.ServiceAccount)
	if sc.citadelManagedObject(acct.GetObjectMeta()) {
		sc.upsertSecret(acct.GetName(), acct.GetNamespace())
	}
	sc.monitoring.ServiceAccountCreation.Increment()
}

// Handles the event where a service account is deleted.
func (sc *SecretController) saDeleted(obj interface{}) {
	acct := obj.(*v1.ServiceAccount)
	sc.deleteSecret(acct.GetName(), acct.GetNamespace())
	sc.monitoring.ServiceAccountDeletion.Increment()
}

func (sc *SecretController) upsertSecret(saName, saNamespace string) {
	secret := k8ssecret.BuildSecret(saName, GetSecretName(saName), saNamespace, nil, nil, nil, nil, nil, IstioSecretType)

	_, exists, err := sc.scrtStore.Get(secret)
	if err != nil {
		k8sControllerLog.Errorf("Failed to get secret %s/%s from the store (error %v)",
			saNamespace, GetSecretName(saName), err)
	}

	if exists {
		// Do nothing for existing secrets. Rotating expiring certs are handled by the `scrtUpdated` method.
		return
	}

	// Now we know the secret does not exist yet. So we create a new one.
	chain, key, err := sc.generateKeyAndCert(saName, saNamespace)
	if err != nil {
		k8sControllerLog.Errorf("Failed to generate key/cert for %s/%s (error %v)",
			saNamespace, GetSecretName(saName), err)
		return
	}
	secret.Data = map[string][]byte{
		CertChainID:  chain,
		PrivateKeyID: key,
		RootCertID:   sc.getTrustAnchorsBundle(),
	}

	// We retry several times when create secret to mitigate transient network failures.
	for i := 0; i < secretCreationRetry; i++ {
		_, err = sc.core.Secrets(saNamespace).Create(secret)
		if err == nil {
			k8sControllerLog.Infof("Secret %s/%s is created successfully", saNamespace, GetSecretName(saName))
			return
		}
		if errors.IsAlreadyExists(err) {
			k8sControllerLog.Infof("Secret %s/%s already exists, skip", saNamespace, GetSecretName(saName))
			return
		}
		k8sControllerLog.Errorf("Failed to create secret %s/%s in attempt %v/%v, (error: %s)",
			saNamespace, GetSecretName(saName), i+1, secretCreationRetry, err)
		time.Sleep(time.Second)
	}
}

func (sc *SecretController) deleteSecret(saName, saNamespace string) {
	err := sc.core.Secrets(saNamespace).Delete(GetSecretName(saName), nil)
	// kube-apiserver returns NotFound error when the secret is successfully deleted.
	if err == nil || errors.IsNotFound(err) {
		k8sControllerLog.Infof("Secret %s/%s deleted successfully", saNamespace, GetSecretName(saName))
		return
	}

	k8sControllerLog.Errorf("Failed to delete secret %s/%s (error: %s)", saNamespace, GetSecretName(saName), err)
}

func (sc *SecretController) scrtDeleted(obj interface{}) {
	scrt, ok := obj.(*v1.Secret)
	if !ok {
		k8sControllerLog.Warnf("Failed to convert to secret object: %v", obj)
		return
	}

	saName := scrt.Annotations[ServiceAccountNameAnnotationKey]
	if sa, err := sc.core.ServiceAccounts(scrt.GetNamespace()).Get(saName, metav1.GetOptions{}); err == nil {
		k8sControllerLog.Infof("Re-creating deleted secret %s/%s.", scrt.GetNamespace(), GetSecretName(saName))
		if sc.citadelManagedObject(sa.GetObjectMeta()) {
			sc.upsertSecret(saName, scrt.GetNamespace())
		}
		sc.monitoring.SecretDeletion.Increment()
	}
}

func (sc *SecretController) namespaceUpdated(oldObj, newObj interface{}) {
	oldNs := oldObj.(*v1.Namespace)
	newNs := newObj.(*v1.Namespace)

	oldManaged := sc.namespaceIsManaged(oldNs)
	newManaged := sc.namespaceIsManaged(newNs)

	if !oldManaged && newManaged {
		sc.enableNamespaceRetroactive(newNs.GetName())
		return
	}
}

// namespaceIsManaged returns whether a given namespace object should be managed by Citadel
func (sc *SecretController) namespaceIsManaged(ns *v1.Namespace) bool {
	nsLabels := ns.GetLabels()

	override, exists := nsLabels[NamespaceOverrideLabel]
	if exists {
		switch override {
		case "true":
			return true
		case "false":
			return false
		} // if exists but not valid, should fall through to check for NamespaceManagedLabel
	}

	targetNamespace, exists := nsLabels[NamespaceManagedLabel]
	if !exists {
		return sc.enableNamespacesByDefault
	}

	if targetNamespace != sc.istioCaStorageNamespace {
		return false // this instance is not the intended CA
	}

	return true
}

// enableNamespaceRetroactive generates secrets for all service accounts in a newly enabled namespace
// for instance: if a namespace had its {NamespaceOverrideLabel: false} label removed, and its namespace is targeted
// in NamespaceManagedLabel, then we should generate secrets for its ServiceAccounts
func (sc *SecretController) enableNamespaceRetroactive(namespace string) {

	serviceAccounts, err := sc.core.ServiceAccounts(namespace).List(metav1.ListOptions{})
	if err != nil {
		k8sControllerLog.Errorf("could not retrieve service account resources from namespace %s", namespace)
		return
	}

	for _, sa := range serviceAccounts.Items {
		sc.upsertSecret(sa.GetName(), sa.GetNamespace()) // idempotent, since does not gen duplicate secrets
	}

}

func (sc *SecretController) generateKeyAndCert(saName string, saNamespace string) ([]byte, []byte, error) {
	id := spiffe.MustGenSpiffeURI(saNamespace, saName)
	if sc.dnsNames != nil {
		// Control plane components in same namespace.
		if e, ok := sc.dnsNames[saName]; ok {
			if e.Namespace == saNamespace {
				// Example: istio-pilot.istio-system.svc, istio-pilot.istio-system
				id += "," + fmt.Sprintf("%s.%s.svc", e.ServiceName, e.Namespace)
				id += "," + fmt.Sprintf("%s.%s", e.ServiceName, e.Namespace)
			}
		}
		// Custom adds more DNS entries using CLI
		if e, ok := sc.dnsNames[saName+"."+saNamespace]; ok {
			for _, d := range e.CustomDomains {
				id += "," + d
			}
		}
	}

	options := util.CertOptions{
		Host:       id,
		RSAKeySize: keySize,
		IsDualUse:  sc.dualUse,
		PKCS8Key:   sc.pkcs8Key,
	}

	csrPEM, keyPEM, err := util.GenCSR(options)
	if err != nil {
		k8sControllerLog.Errorf("CSR generation error (%v)", err)
		sc.monitoring.CSRError.Increment()
		return nil, nil, err
	}

	certChainPEM := sc.ca.GetCAKeyCertBundle().GetCertChainPem()
	certPEM, signErr := sc.ca.Sign(csrPEM, strings.Split(id, ","), sc.certTTL, sc.forCA)
	if signErr != nil {
		k8sControllerLog.Errorf("CSR signing error (%v)", signErr.Error())
		sc.monitoring.GetCertSignError(signErr.(*caerror.Error).ErrorType()).Increment()
		return nil, nil, fmt.Errorf("CSR signing error (%v)", signErr.(*caerror.Error))
	}
	certPEM = append(certPEM, certChainPEM...)

	return certPEM, keyPEM, nil
}

func (sc *SecretController) scrtUpdated(oldObj, newObj interface{}) {
	scrt, ok := newObj.(*v1.Secret)
	if !ok {
		k8sControllerLog.Warnf("Failed to convert to secret object: %v", newObj)
		return
	}
	namespace := scrt.GetNamespace()
	name := scrt.GetName()

	_, waitErr := sc.certUtil.GetWaitTime(scrt.Data[CertChainID], time.Now(), sc.minGracePeriod)

	rootCertificates := sc.getTrustAnchorsBundle()

	// Refresh the secret if 1) the certificate contained in the secret is about
	// to expire, or 2) the root certificates in the secret are different than the
	// one held by the ca (this may happen when the CA is restarted and
	// a new self-signed CA cert is generated).
	if waitErr != nil || !bytes.Equal(rootCertificates, scrt.Data[RootCertID]) {
		// if the namespace is not managed, don't refresh the expired secret, delete it
		secretNamespace, err := sc.core.Namespaces().Get(namespace, metav1.GetOptions{})
		if err == nil {
			if !sc.namespaceIsManaged(secretNamespace) { // delete the expiring secret if namespace not Citadel managed
				sc.deleteSecret(name, namespace)
				return
			}
		} else { // in the case we couldn't retrieve namespace, we should proceed with cert refresh
			k8sControllerLog.Errorf("Failed to retrieve details for namespace %s, err %v", namespace, err)
		}

		if waitErr != nil {
			k8sControllerLog.Infof("Refreshing about to expire secret %s/%s: %s", namespace, GetSecretName(name), waitErr.Error())
		} else {
			k8sControllerLog.Infof("Refreshing secret %s/%s (outdated root cert)", namespace, GetSecretName(name))
		}

		if err = sc.refreshSecret(scrt); err != nil {
			k8sControllerLog.Errorf("Failed to refresh secret %s/%s (error: %s)", namespace, GetSecretName(name), err)
		} else {
			k8sControllerLog.Infof("Secret %s/%s refreshed successfully.", namespace, GetSecretName(name))
		}
	}
}

// refreshSecret is an inner func to refresh cert secrets when necessary
func (sc *SecretController) refreshSecret(scrt *v1.Secret) error {
	namespace := scrt.GetNamespace()
	saName := scrt.Annotations[ServiceAccountNameAnnotationKey]

	chain, key, err := sc.generateKeyAndCert(saName, namespace)
	if err != nil {
		return err
	}

	scrt.Data[CertChainID] = chain
	scrt.Data[PrivateKeyID] = key
	scrt.Data[RootCertID] = sc.getTrustAnchorsBundle()

	_, err = sc.core.Secrets(namespace).Update(scrt)
	return err
}

func (sc *SecretController) refreshTrustAnchorsBundle() {
	trustAnchorsSet := map[string]struct{}{}

	sc.primaryTrustAnchor = sc.ca.GetCAKeyCertBundle().GetRootCertPem()
	primary := strings.TrimSpace(string(sc.primaryTrustAnchor))
	trustAnchorsSet[primary] = struct{}{}

	for _, configMap := range sc.extraTrustAnchors {
		for _, value := range configMap {
			cert := strings.TrimSpace(value)
			trustAnchorsSet[cert] = struct{}{}
		}
	}

	trustAnchors := make([]string, 0, len(trustAnchorsSet))
	for trustAnchor := range trustAnchorsSet {
		trustAnchors = append(trustAnchors, trustAnchor)
	}
	sort.Strings(trustAnchors)

	sc.combinedTrustAnchorsBundle = []byte(strings.Join(trustAnchors, "\n") + "\n")
}

func (sc *SecretController) getTrustAnchorsBundle() []byte {
	// TODO trigger refresh on CA secret change instead of performing this check on every get
	if !reflect.DeepEqual(sc.primaryTrustAnchor, sc.ca.GetCAKeyCertBundle().GetRootCertPem()) {
		sc.refreshTrustAnchorsBundle()
	}
	return sc.combinedTrustAnchorsBundle
}
