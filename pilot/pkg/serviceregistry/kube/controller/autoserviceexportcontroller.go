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

package controller

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	mcsapi "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/model"
	serviceRegistryKube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/kube/mcs"
)

type autoServiceExportController struct {
	autoServiceExportOptions

	client   kube.Client
	queue    controllers.Queue
	services kclient.Client[*v1.Service]

	// We use this flag to short-circuit the logic and stop the controller
	// if the CRD does not exist (or is deleted)
	mcsSupported bool
}

// autoServiceExportOptions provide options for creating a autoServiceExportController.
type autoServiceExportOptions struct {
	Client       kube.Client
	ClusterID    cluster.ID
	DomainSuffix string
	ClusterLocal model.ClusterLocalProvider
}

// newAutoServiceExportController creates a new autoServiceExportController.
func newAutoServiceExportController(opts autoServiceExportOptions) *autoServiceExportController {
	c := &autoServiceExportController{
		autoServiceExportOptions: opts,
		client:                   opts.Client,
		mcsSupported:             true,
	}
	c.queue = controllers.NewQueue("auto export",
		controllers.WithReconciler(c.Reconcile),
		controllers.WithMaxAttempts(5))

	c.services = kclient.NewFiltered[*v1.Service](opts.Client, kubetypes.Filter{ObjectFilter: opts.Client.ObjectFilter()})

	// Only handle add. The controller only acts on parts of the service
	// that are immutable (e.g. name). When we create ServiceExport, we bind its
	// lifecycle to the Service so that when the Service is deleted,
	// k8s automatically deletes the ServiceExport.
	c.services.AddEventHandler(controllers.EventHandler[controllers.Object]{AddFunc: c.queue.AddObject})

	return c
}

func (c *autoServiceExportController) Run(stopCh <-chan struct{}) {
	kube.WaitForCacheSync("auto service export", stopCh, c.services.HasSynced)
	c.queue.Run(stopCh)
	c.services.ShutdownHandlers()
}

func (c *autoServiceExportController) logPrefix() string {
	return "AutoServiceExport (cluster=" + c.ClusterID.String() + ") "
}

// func (c *autoServiceExportController) createServiceExportIfNotPresent(svc *v1.Service) error {
func (c *autoServiceExportController) Reconcile(key types.NamespacedName) error {
	if !c.mcsSupported {
		// Don't create ServiceExport if MCS is not supported on the cluster.
		log.Debugf("%s ignoring added Service, since !mcsSupported", c.logPrefix())
		return nil
	}

	svc := c.services.Get(key.Name, key.Namespace)
	if svc == nil {
		// Service no longer exists, no action needed
		return nil
	}

	if c.isClusterLocalService(svc) {
		// Don't create ServiceExport if the service is configured to be
		// local to the cluster (i.e. non-exported).
		log.Debugf("%s ignoring cluster-local service %s/%s", c.logPrefix(), svc.Namespace, svc.Name)
		return nil
	}
	serviceExport := mcsapi.ServiceExport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceExport",
			APIVersion: mcs.MCSSchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: svc.Namespace,
			Name:      svc.Name,

			// Bind the lifecycle of the ServiceExport to the Service. We do this by making the Service
			// the "owner" of the ServiceExport resource.
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: v1.SchemeGroupVersion.String(),
					Kind:       gvk.Service.Kind,
					Name:       svc.Name,
					UID:        svc.UID,
				},
			},
		},
	}

	// Convert to unstructured.
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&serviceExport)
	if err != nil {
		log.Warnf("%s failed converting ServiceExport %s/%s to Unstructured: %v", c.logPrefix(),
			svc.Namespace, svc.Name, err)
		return err
	}

	if _, err = c.client.Dynamic().Resource(mcs.ServiceExportGVR).Namespace(serviceExport.Namespace).Create(
		context.TODO(), &unstructured.Unstructured{Object: u}, metav1.CreateOptions{}); err != nil {
		switch {
		case errors.IsAlreadyExists(err):
			// The ServiceExport already exists. Nothing to do.
			return nil
		case errors.IsNotFound(err):
			log.Warnf("%s ServiceExport CRD Not found. Shutting down MCS ServiceExport sync. "+
				"Please add the CRD then restart the istiod deployment", c.logPrefix())
			c.mcsSupported = false

			// Do not return the error, so that the queue does not attempt a retry.
			return nil
		}
	}

	if err != nil {
		log.Warnf("%s failed creating ServiceExport %s/%s: %v", c.logPrefix(), svc.Namespace, svc.Name, err)
		return err
	}

	log.Debugf("%s created ServiceExport %s/%s", c.logPrefix(), svc.Namespace, svc.Name)
	return nil
}

func (c *autoServiceExportController) isClusterLocalService(svc *v1.Service) bool {
	hostname := serviceRegistryKube.ServiceHostname(svc.Name, svc.Namespace, c.DomainSuffix)
	return c.ClusterLocal.GetClusterLocalHosts().IsClusterLocal(hostname)
}
