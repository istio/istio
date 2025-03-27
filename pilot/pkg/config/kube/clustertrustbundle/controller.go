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

package clustertrustbundle

import (
	certificatesv1alpha1 "k8s.io/api/certificates/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
)

const (
	// The name of the ClusterTrustBundle resource that will store Istio's root certificate
	istioClusterTrustBundleName = "istio.io:istiod-ca:root-cert"

	// The signerName of the ClusterTrustBundle
	istioClusterTrustBundleSignerName = "istio.io/istiod-ca"

	// maxRetries is the number of times a ClusterTrustBundle reconciliation will be retried before it is dropped
	// out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuing of a namespace.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms
	maxRetries = 5
)

// Controller manages the lifecycle of the Istio root certificate in ClusterTrustBundle
// It watches Istio's KeyCert bundle as well as ClusterTrustBundles in the cluster, and it updates ClusterTrustBundles whenever
// the certificate is rotated. Later on, we might want to add reading from other ClusterTrustBundles as well
type Controller struct {
	caBundleWatcher     *keycertbundle.Watcher
	queue               controllers.Queue
	clustertrustbundles kclient.Client[*certificatesv1alpha1.ClusterTrustBundle]
}

// NewController creates a new ClusterTrustBundleController
func NewController(kubeClient kube.Client, caBundleWatcher *keycertbundle.Watcher) *Controller {
	c := &Controller{
		caBundleWatcher: caBundleWatcher,
	}

	c.queue = controllers.NewQueue("clustertrustbundle controller",
		controllers.WithReconciler(c.reconcileClusterTrustBundle),
		controllers.WithMaxAttempts(maxRetries))

	c.clustertrustbundles = kclient.NewFiltered[*certificatesv1alpha1.ClusterTrustBundle](kubeClient, kclient.Filter{
		FieldSelector: "metadata.name=" + istioClusterTrustBundleName,
		ObjectFilter:  kubeClient.ObjectFilter(),
	})

	c.clustertrustbundles.AddEventHandler(controllers.FilteredObjectSpecHandler(c.queue.AddObject, func(o controllers.Object) bool { return true }))

	return c
}

// startCaBundleWatcher listens for updates to the CA bundle and queues a reconciliation of the ClusterTrustBundle
func (c *Controller) startCaBundleWatcher(stop <-chan struct{}) {
	id, watchCh := c.caBundleWatcher.AddWatcher()
	defer c.caBundleWatcher.RemoveWatcher(id)
	for {
		select {
		case <-watchCh:
			c.queue.AddObject(&certificatesv1alpha1.ClusterTrustBundle{
				ObjectMeta: metav1.ObjectMeta{
					Name: istioClusterTrustBundleName,
				},
			})
		case <-stop:
			return
		}
	}
}

func (c *Controller) reconcileClusterTrustBundle(o types.NamespacedName) error {
	if o.Name == istioClusterTrustBundleName {
		return c.updateClusterTrustBundle(c.caBundleWatcher.GetCABundle())
	}
	return nil
}

// Run starts the controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync("clustertrustbundle controller", stopCh, c.clustertrustbundles.HasSynced) {
		return
	}
	go c.startCaBundleWatcher(stopCh)

	// queue an initial event
	c.queue.AddObject(&certificatesv1alpha1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{
			Name: istioClusterTrustBundleName,
		},
	})

	c.queue.Run(stopCh)
	controllers.ShutdownAll(c.clustertrustbundles)
}

// updateClusterTrustBundle updates the root certificate in the ClusterTrustBundle
func (c *Controller) updateClusterTrustBundle(rootCert []byte) error {
	bundle := &certificatesv1alpha1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{
			Name: istioClusterTrustBundleName,
		},
		Spec: certificatesv1alpha1.ClusterTrustBundleSpec{
			SignerName:  istioClusterTrustBundleSignerName,
			TrustBundle: string(rootCert),
		},
	}

	existing := c.clustertrustbundles.Get(istioClusterTrustBundleName, "")
	if existing != nil {
		if existing.Spec.TrustBundle == string(rootCert) {
			// trustbundle is up to date. nothing to do
			return nil
		}
		// Update existing bundle
		existing.Spec.TrustBundle = string(rootCert)
		_, err := c.clustertrustbundles.Update(existing)
		return err
	}

	// Create new bundle
	_, err := c.clustertrustbundles.Create(bundle)
	return err
}
