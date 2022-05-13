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
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	mcsapi "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/model"
	serviceRegistryKube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/mcs"
	"istio.io/istio/pkg/queue"
)

type autoServiceExportController struct {
	autoServiceExportOptions

	client          kube.Client
	queue           queue.Instance
	serviceInformer cache.SharedInformer

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
		queue:                    queue.NewQueue(time.Second),
		mcsSupported:             true,
	}

	log.Infof("%s starting controller", c.logPrefix())

	c.serviceInformer = opts.Client.KubeInformer().Core().V1().Services().Informer()
	c.serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { c.onServiceAdd(obj) },

		// Do nothing on update. The controller only acts on parts of the service
		// that are immutable (e.g. name).

		// Do nothing on delete. When we create ServiceExport, we bind its
		// lifecycle to the Service so that when the Service is deleted,
		// k8s automatically deletes the ServiceExport.
	})

	return c
}

func (c *autoServiceExportController) onServiceAdd(obj interface{}) {
	c.queue.Push(func() error {
		if !c.mcsSupported {
			// Don't create ServiceExport if MCS is not supported on the cluster.
			log.Debugf("%s ignoring added Service, since !mcsSupported", c.logPrefix())
			return nil
		}

		svc, err := extractService(obj)
		if err != nil {
			log.Warnf("%s failed converting service: %v", c.logPrefix(), err)
			return err
		}

		if c.isClusterLocalService(svc) {
			// Don't create ServiceExport if the service is configured to be
			// local to the cluster (i.e. non-exported).
			log.Debugf("%s ignoring cluster-local service %s/%s", c.logPrefix(), svc.Namespace, svc.Name)
			return nil
		}

		return c.createServiceExportIfNotPresent(svc)
	})
}

func (c *autoServiceExportController) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync(stopCh, c.serviceInformer.HasSynced) {
		log.Errorf("%s failed to sync cache", c.logPrefix())
		return
	}
	log.Infof("%s started", c.logPrefix())
	go c.queue.Run(stopCh)
}

func (c *autoServiceExportController) logPrefix() string {
	return "AutoServiceExport (cluster=" + c.ClusterID.String() + ") "
}

func (c *autoServiceExportController) createServiceExportIfNotPresent(svc *v1.Service) error {
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
					Kind:       "Service",
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
