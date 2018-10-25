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

package validation

import (
	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/log"
)

type endpointReadiness int

const (
	endpointCheckShutdown endpointReadiness = iota
	endpointCheckReady
	endpointCheckNotReady
)

func endpointReady(store cache.Store, queue workqueue.RateLimitingInterface, namespace, name string) endpointReadiness {
	key, quit := queue.Get()
	if quit {
		return endpointCheckShutdown
	}
	defer queue.Done(key)

	item, exists, err := store.GetByKey(key.(string))
	if err != nil || !exists {
		return endpointCheckNotReady
	}
	endpoints := item.(*v1.Endpoints)
	if len(endpoints.Subsets) == 0 {
		log.Warnf("%s/%v endpoint not ready: no subsets", namespace, name)
		return endpointCheckNotReady
	}
	for _, subset := range endpoints.Subsets {
		if len(subset.Addresses) > 0 {
			return endpointCheckReady
		}
	}
	log.Warnf("%s/%v endpoint not ready: no ready addresses", namespace, name)
	return endpointCheckNotReady
}

func (wh *Webhook) waitForEndpointReady(stopCh <-chan struct{}) (shutdown bool) {
	log.Infof("Checking if %s/%s is ready before registering webhook configuration ",
		wh.deploymentAndServiceNamespace, wh.deploymentName)

	defer func() {
		if shutdown {
			log.Info("Endpoint readiness check stopped - controller shutting down")
		} else {
			log.Infof("Endpoint %s/%s is ready", wh.deploymentAndServiceNamespace, wh.deploymentName)
		}
	}()

	controllerStopCh := make(chan struct{})
	defer close(controllerStopCh)

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	defer queue.ShutDown()

	store, controller := cache.NewInformer(
		wh.createInformerEndpointSource(wh.clientset, wh.deploymentAndServiceNamespace, wh.serviceName),
		&v1.Endpoints{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if key, err := cache.MetaNamespaceKeyFunc(obj); err == nil {
					queue.Add(key)
				}
			},
			UpdateFunc: func(prev, curr interface{}) {
				prevObj := prev.(*v1.Endpoints)
				currObj := curr.(*v1.Endpoints)
				if prevObj.ResourceVersion != currObj.ResourceVersion {
					if key, err := cache.MetaNamespaceKeyFunc(curr); err == nil {
						queue.Add(key)
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj); err == nil {
					queue.Add(key)
				}
			},
		},
	)
	go controller.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, controller.HasSynced) {
		return true
	}

	for {
		ready := endpointReady(store, queue, wh.deploymentAndServiceNamespace, wh.serviceName)
		switch ready {
		case endpointCheckShutdown:
			return true
		case endpointCheckReady:
			return false
		case endpointCheckNotReady:
			// continue waiting for endpoint to be ready
		}
	}
}
