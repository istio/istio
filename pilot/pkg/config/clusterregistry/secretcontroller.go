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

package clusterregistry

import (
	"fmt"
	"strings"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	// "k8s.io/client-go/tools/clientcmd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/log"
)

const (
	mcLabel = "istio/multiCluster"
)

// Controller is the controller implementation for Secret resources
type Controller struct {
	kubeclientset kubernetes.Interface
	namespace     string
	secretsSynced cache.InformerSynced
	cs            *ClusterStore
}

// NewController returns a new secret controller
func NewController(
	kubeclientset kubernetes.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	namespace string,
	cs *ClusterStore) *Controller {

	secretsInformer := kubeInformerFactory.Core().V1().Secrets()

	controller := &Controller{
		kubeclientset: kubeclientset,
		namespace:     namespace,
		secretsSynced: secretsInformer.Informer().HasSynced,
		cs:            cs,
	}

	log.Info("Setting up event handlers")
	secretsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.secretAdd,
		DeleteFunc: controller.secretDelete,
	})

	return controller
}

// Run starts the controller until it receves a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()

	// Start the informer factories to begin populating the informer caches
	log.Info("Starting Secrets controller")

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.secretsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

func checkSecret(s *corev1.Secret) bool {
	lv, ok := s.Labels[mcLabel]
	if !ok {
		return false
	}
	if strings.ToLower(lv) != "true" {
		return false
	}

	return ok
}

func addMemberCluster(s *corev1.Secret, cs *ClusterStore) {
	cs.storeLock.Lock()
	defer cs.storeLock.Unlock()
	// Check if there is already a cluster member with the specified
	if _, ok := cs.clientConfigs[s.ObjectMeta.Name]; !ok {
		log.Infof("Adding new cluster member: %s", s.ObjectMeta.Name)
		// cs.store[s.ObjectMeta.Annotations[mcAnnotation]] = s.Data[s.ObjectMeta.Annotations[mcAnnotation]]
	}
	log.Infof("Number of clusters in the cluster store: %d", len(cs.clientConfigs))
}

func deleteMemberCluster(s *corev1.Secret, cs *ClusterStore) {
	cs.storeLock.Lock()
	defer cs.storeLock.Unlock()
	// Check if there is a cluster member with the specified name
	if _, ok := cs.clientConfigs[s.ObjectMeta.Name]; ok {
		log.Infof("Deleting cluster member: %s", s.ObjectMeta.Name)
		delete(cs.clientConfigs, s.ObjectMeta.Name)
	}
	log.Infof("Number of clusters in the cluster store: %d", len(cs.clientConfigs))
}

func (c *Controller) secretAdd(obj interface{}) {
	s := obj.(*corev1.Secret)
	if s.ObjectMeta.Namespace == c.namespace {
		if checkSecret(s) {
			addMemberCluster(s, c.cs)
		}
	}
}

func (c *Controller) secretDelete(obj interface{}) {
	s := obj.(*corev1.Secret)
	if s.ObjectMeta.Namespace == c.namespace {
		if checkSecret(s) {
			deleteMemberCluster(s, c.cs)
		}
	}
}

// TODO Add Secret Update handler, it will allow detect Secret key rotation

// StartSecretController start k8s controller which will be watching Secret object
// in a specified namesapce
func StartSecretController(k8s kubernetes.Interface, cs *ClusterStore, namespace string) error {

	stopCh := make(chan struct{})

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(k8s, time.Second*30)
	controller := NewController(k8s, kubeInformerFactory, namespace, cs)

	go kubeInformerFactory.Start(stopCh)
	if err := controller.Run(stopCh); err != nil {
		log.Errorf("Error running Secret controller: %s", err.Error())
		return err
	}

	return nil
}
