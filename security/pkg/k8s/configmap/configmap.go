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

package configmap

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/security/pkg/pki/util"
)

var configMapLog = log.RegisterScope("configMapController", "ConfigMap controller log", 0)

const (
	IstioSecurityConfigMapName = "istio-security"
	CATLSRootCertName          = "caTLSRootCert"

	// ConfigMap label key that indicates that the ConfigMap contains additional trusted CA roots.
	ExtraTrustAnchorsLabel = "security.istio.io/extra-trust-anchors"

	// periodically refresh the primaryTrustAnchor from the CA.
	PrimaryTrustAnchorRefreshTimeout = time.Second
)

// CertificateAuthority contains methods to be supported by a CA.
type CertificateAuthority interface {
	// Sign generates a certificate for a workload or CA, from the given CSR and TTL.
	// TODO(myidpt): simplify this interface and pass a struct with cert field values instead.
	Sign(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error)
	// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain.
	SignWithCertChain(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error)
	// GetCAKeyCertBundle returns the KeyCertBundle used by CA.
	GetCAKeyCertBundle() util.KeyCertBundle
}

// Controller manages the CA TLS root cert in ConfigMap.
type Controller struct {
	core      corev1.CoreV1Interface
	namespace string

	ca                          CertificateAuthority
	monitoring                  monitoringMetrics
	extraTrustAnchorsController cache.Controller
	extraTrustAnchorMu          sync.Mutex
	// extra trust anchors organized by source configmap name and key within that configmap.
	// The value is a PEM encoded root certificate. Extra anchors are combined with the
	// primary root to form the list of trusted roots distributed to proxies.
	extraTrustAnchors map[string]map[string]string // configmap name -> configmap key -> PEM encoded cert
	// Keep a copy of the primary trust anchor so we know when to rebuild
	// the combined list of trust anchors.
	primaryTrustAnchor []byte
	// periodically refresh the primaryTrustAnchor from the CA.
	lastPrimaryTrustAnchorCheck time.Time
	// Combined list of the primary root CA plus any additional trust
	// anchors that were added by the user. This is recomputed whenever the
	// primary root changes or when additional trust anchors are added/removed.
	combinedTrustAnchorsBundle []byte
}

// NewController creates a new Controller.
func NewController(namespace string, core corev1.CoreV1Interface, watchTrustAnchors bool, ca CertificateAuthority) (*Controller, error) {
	c := &Controller{
		namespace:  namespace,
		core:       core,
		ca:         ca,
		monitoring: newMonitoringMetrics(),
	}

	if watchTrustAnchors {
		// Extra trust anchors related routines. Holding the lock isn't strictly necessary since
		// the controller hasn't started yet. It's added for completeness.
		c.extraTrustAnchorMu.Lock()
		c.lastPrimaryTrustAnchorCheck = time.Now()
		c.extraTrustAnchors = map[string]map[string]string{}
		c.refreshTrustAnchorsBundle()
		c.extraTrustAnchorMu.Unlock()

		extraTrustAnchorsSelector := labels.SelectorFromSet(map[string]string{ExtraTrustAnchorsLabel: "true"}).String()
		extraTrustAnchorsLW := &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = extraTrustAnchorsSelector
				return core.ConfigMaps(namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = extraTrustAnchorsSelector
				return core.ConfigMaps(namespace).Watch(options)
			},
		}
		_, c.extraTrustAnchorsController = cache.NewInformer(extraTrustAnchorsLW, &v1.ConfigMap{}, time.Minute, cache.ResourceEventHandlerFuncs{
			AddFunc: func(newObj interface{}) {
				newConfigMap := newObj.(*v1.ConfigMap)

				log.Infof("Notified about a new ConfigMap (%v) containing extra trust anchors",
					newConfigMap.GetName())
				c.updateExtraTrustAnchor(newConfigMap.GetName(), newConfigMap.Data)
			},
			DeleteFunc: func(oldObj interface{}) {
				oldConfigMap := oldObj.(*v1.ConfigMap)

				log.Infof("Notified about a deletion of an existing ConfigMap (%v) containing extra trust anchors",
					oldConfigMap.GetName())
				c.removeExtraTrustAnchor(oldConfigMap.GetName())
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldConfigMap := oldObj.(*v1.ConfigMap)
				newConfigMap := newObj.(*v1.ConfigMap)

				if oldConfigMap.ResourceVersion != newConfigMap.ResourceVersion {
					log.Infof("Notified about an update to an existing ConfigMap (%v) containing extra trust anchors",
						oldConfigMap.GetName())
					c.updateExtraTrustAnchor(newConfigMap.GetName(), newConfigMap.Data)
				}
			},
		})
	}

	return c, nil
}

// InsertCATLSRootCert updates the CA TLS root certificate in the configmap.
func (c *Controller) InsertCATLSRootCert(value string) error {
	configmap, err := c.core.ConfigMaps(c.namespace).Get(IstioSecurityConfigMapName, metav1.GetOptions{})
	exists := true
	if err != nil {
		if errors.IsNotFound(err) {
			// Create a new ConfigMap.
			configmap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      IstioSecurityConfigMapName,
					Namespace: c.namespace,
				},
				Data: map[string]string{},
			}
			exists = false
		} else {
			return fmt.Errorf("failed to insert CA TLS root cert: %v", err)
		}
	}
	configmap.Data[CATLSRootCertName] = value
	if exists {
		if _, err = c.core.ConfigMaps(c.namespace).Update(configmap); err != nil {
			return fmt.Errorf("failed to insert CA TLS root cert: %v", err)
		}
	} else {
		if _, err = c.core.ConfigMaps(c.namespace).Create(configmap); err != nil {
			return fmt.Errorf("failed to insert CA TLS root cert: %v", err)
		}
	}
	return nil
}

// InsertCATLSRootCertWithRetry updates the CA TLS root certificate in the configmap with
// retries until timeout.
func (c *Controller) InsertCATLSRootCertWithRetry(value string, retryInterval,
	timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := c.InsertCATLSRootCert(value)
	ticker := time.NewTicker(retryInterval)
	for err != nil {
		configMapLog.Errorf("Failed to update root cert in config map: %s", err.Error())
		select {
		case <-ticker.C:
			if err = c.InsertCATLSRootCert(value); err == nil {
				break
			}
		case <-ctx.Done():
			configMapLog.Error("Failed to update root cert in config map until timeout.")
			ticker.Stop()
			return err
		}
	}
	return nil
}

// GetCATLSRootCert gets the CA TLS root certificate from the configmap.
func (c *Controller) GetCATLSRootCert() (string, error) {
	configmap, err := c.core.ConfigMaps(c.namespace).Get(IstioSecurityConfigMapName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get CA TLS root cert: %v", err)
	}
	rootCert := configmap.Data[CATLSRootCertName]
	if rootCert == "" {
		return "", fmt.Errorf("failed to get CA TLS root cert from configmap %s:%s",
			IstioSecurityConfigMapName, CATLSRootCertName)
	}

	return rootCert, nil
}

func (c *Controller) Run(stopCh chan struct{}) {
	if c.extraTrustAnchorsController != nil {
		c.extraTrustAnchorsController.Run(stopCh)
	}
}

func (c *Controller) WaitForSync(stopCh chan struct{}) {
	if c.extraTrustAnchorsController != nil {
		cache.WaitForCacheSync(stopCh, c.extraTrustAnchorsController.HasSynced)
	}
}

func (c *Controller) RefreshTrustAnchorsBundle() []byte {
	c.extraTrustAnchorMu.Lock()
	defer c.extraTrustAnchorMu.Unlock()
	return c.refreshTrustAnchorsBundle()
}

// The caller is responsible for holding the sc.extraTrustAnchorMu lock while calling this method.
func (c *Controller) refreshTrustAnchorsBundle() []byte {
	trustAnchorsSet := map[string]struct{}{}

	c.primaryTrustAnchor = c.ca.GetCAKeyCertBundle().GetRootCertPem()
	primary := strings.TrimSpace(string(c.primaryTrustAnchor))
	trustAnchorsSet[primary] = struct{}{}

	for _, configMap := range c.extraTrustAnchors {
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

	c.combinedTrustAnchorsBundle = []byte(strings.Join(trustAnchors, "\n"))

	return c.combinedTrustAnchorsBundle
}

var TimeNowTestStub = time.Now

func (c *Controller) GetTrustAnchorsBundle() []byte {
	c.extraTrustAnchorMu.Lock()
	defer c.extraTrustAnchorMu.Unlock()

	if c.extraTrustAnchorsController == nil {
		return c.primaryTrustAnchor
	}

	now := TimeNowTestStub()
	if true || now.After(c.lastPrimaryTrustAnchorCheck.Add(PrimaryTrustAnchorRefreshTimeout)) {
		c.lastPrimaryTrustAnchorCheck = now
		c.refreshTrustAnchorsBundle()
	}
	return c.combinedTrustAnchorsBundle
}

func (c *Controller) updateExtraTrustAnchor(name string, data map[string]string) {
	if c.extraTrustAnchorsController == nil {
		return
	}
	c.extraTrustAnchorMu.Lock()
	defer c.extraTrustAnchorMu.Unlock()

	c.extraTrustAnchors[name] = data
	c.refreshTrustAnchorsBundle()

	c.monitoring.extraTrustAnchors.Record(float64(len(c.extraTrustAnchors)))
}

func (c *Controller) removeExtraTrustAnchor(name string) {
	if c.extraTrustAnchorsController == nil {
		return
	}
	c.extraTrustAnchorMu.Lock()
	defer c.extraTrustAnchorMu.Unlock()

	delete(c.extraTrustAnchors, name)
	c.refreshTrustAnchorsBundle()

	c.monitoring.extraTrustAnchors.Record(float64(len(c.extraTrustAnchors)))
}
