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
	"context"

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
	istioClusterTrustBundleName = "istio-ca-root-cert"

	// maxRetries is the number of times a ClusterTrustBundle reconciliation will be retried before it is dropped
	// out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuing of a namespace.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms
	maxRetries = 5
)

// ClusterTrustBundleController manages the lifecycle of the Istio root certificate in ClusterTrustBundle
// It watches Istio's KeyCert bundle as well as ClusterTrustBundles in the cluster, and it updates ClusterTrustBundles whenever
// the certificate is rotated. Later on, we might want to add reading from other ClusterTrustBundles as well
type ClusterTrustBundleController struct {
	client              kube.Client
	caBundleWatcher     *keycertbundle.Watcher
	queue               controllers.Queue
	clustertrustbundles kclient.Client[*certificatesv1alpha1.ClusterTrustBundle]
	stopChan            chan struct{}
}

// NewClusterTrustBundleController creates a new ClusterTrustBundleController
func NewClusterTrustBundleController(kubeClient kube.Client, caBundleWatcher *keycertbundle.Watcher) *ClusterTrustBundleController {
	c := &ClusterTrustBundleController{
		client:          kubeClient,
		stopChan:        make(chan struct{}),
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
func (c *ClusterTrustBundleController) startCaBundleWatcher(stop <-chan struct{}) {
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

func (c *ClusterTrustBundleController) reconcileClusterTrustBundle(o types.NamespacedName) error {
	if o.Name == istioClusterTrustBundleName {
		return c.updateClusterTrustBundle(c.caBundleWatcher.GetCABundle())
	}
	return nil
}

// Run starts the controller
func (c *ClusterTrustBundleController) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync("clustertrustbundle controller", stopCh, c.clustertrustbundles.HasSynced) {
		return
	}
	go c.startCaBundleWatcher(stopCh)
	defer close(c.stopChan)

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
func (c *ClusterTrustBundleController) updateClusterTrustBundle(rootCert []byte) error {
	bundle := &certificatesv1alpha1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{
			Name: istioClusterTrustBundleName,
		},
		Spec: certificatesv1alpha1.ClusterTrustBundleSpec{
			TrustBundle: string(rootCert),
		},
	}

	existing, err := c.client.Kube().CertificatesV1alpha1().ClusterTrustBundles().Get(context.Background(), istioClusterTrustBundleName, metav1.GetOptions{})
	if err == nil {
		// Update existing bundle
		existing.Spec.TrustBundle = string(rootCert)
		_, err = c.client.Kube().CertificatesV1alpha1().ClusterTrustBundles().Update(context.Background(), existing, metav1.UpdateOptions{})
		return err
	}

	// Create new bundle
	_, err = c.client.Kube().CertificatesV1alpha1().ClusterTrustBundles().Create(context.Background(), bundle, metav1.CreateOptions{})
	return err
}
