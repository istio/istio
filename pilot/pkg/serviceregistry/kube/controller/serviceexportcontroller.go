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
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"

	"istio.io/istio/pilot/pkg/model"
	serviceRegistryKube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/queue"
	"istio.io/pkg/log"
)

type ServiceExportController struct {
	ServiceExportOptions
	client        versioned.Interface
	serviceClient corev1.CoreV1Interface

	queue           queue.Instance
	serviceInformer cache.SharedInformer

	// We use this flag to short-circuit the logic and stop the controller
	// if the CRD does not exist (or is deleted)
	mcsSupported bool
}

// ServiceExportOptions provide options for creating a ServiceExportController.
type ServiceExportOptions struct {
	Client       kube.Client
	ClusterID    string
	DomainSuffix string
	ClusterLocal model.ClusterLocalProvider
}

// NewServiceExportController creates a new ServiceExportController.
func NewServiceExportController(opts ServiceExportOptions) *ServiceExportController {
	c := &ServiceExportController{
		ServiceExportOptions: opts,
		client:               opts.Client.MCSApis(),
		serviceClient:        opts.Client.Kube().CoreV1(),
		queue:                queue.NewQueue(time.Second),
		mcsSupported:         true,
	}

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

func (c *ServiceExportController) onServiceAdd(obj interface{}) {
	c.queue.Push(func() error {
		if !c.mcsSupported {
			// Don't create ServiceExport if MCS is not supported on the cluster.
			return nil
		}

		svc, err := convertToService(obj)
		if err != nil {
			return err
		}

		if c.isClusterLocal(svc) {
			// Don't create ServiceExport if the service is configured to be
			// local to the cluster (i.e. non-exported).
			return nil
		}

		return c.createServiceExportIfNotPresent(svc)
	})
}

func (c *ServiceExportController) Run(stopCh <-chan struct{}) {
	cache.WaitForCacheSync(stopCh, c.serviceInformer.HasSynced)
	log.Infof("ServiceExport controller started")
	go c.queue.Run(stopCh)
}

func (c *ServiceExportController) createServiceExportIfNotPresent(svc *v1.Service) error {
	serviceExport := v1alpha1.ServiceExport{}
	serviceExport.Namespace = svc.Namespace
	serviceExport.Name = svc.Name

	// Bind the lifecycle of the ServiceExport to the Service. We do this by making the Service
	// the "owner" of the ServiceExport resource.
	serviceExport.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "Service",
			Name:       svc.Name,
			UID:        svc.UID,
		},
	}

	serviceExports := c.client.MulticlusterV1alpha1().ServiceExports(svc.Namespace)
	_, err := serviceExports.Create(context.TODO(), &serviceExport, metav1.CreateOptions{})
	if err != nil {
		switch {
		case errors.IsAlreadyExists(err):
			// The ServiceExport already exists. Nothing to do.
			return nil
		case errors.IsNotFound(err):
			log.Errorf("ServiceExport CRD Not found in cluster %s. Shutting down MCS ServiceExport sync. "+
				"Please add the CRD then restart the istiod deployment", c.ClusterID)
			c.mcsSupported = false

			// Do not return the error, so that the queue does not attempt a retry.
			return nil
		}
	}
	return err
}

func (c *ServiceExportController) isClusterLocal(svc *v1.Service) bool {
	hostname := serviceRegistryKube.ServiceHostname(svc.Name, svc.Namespace, c.DomainSuffix)
	return c.ClusterLocal.GetClusterLocalHosts().IsClusterLocal(hostname)
}
