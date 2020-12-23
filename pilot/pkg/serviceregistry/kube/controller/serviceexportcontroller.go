package controller

import (
	"context"
	"fmt"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/queue"
	"istio.io/pkg/log"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"
	"strings"
	"time"
)

type ServiceExportController struct {
	client versioned.Interface
	serviceClient corev1.CoreV1Interface

	queue              queue.Instance
	serviceInformer  cache.SharedInformer

	clusterLocalHosts []string //hosts marked as cluster-local, which will not have serviceeexports generated
}

func NewServiceExportController(kubeClient kube.Client, clusterLocalHosts []string) (*ServiceExportController, error) {
	serviceExportController := &ServiceExportController{
		client: kubeClient.MCSApis(),
		serviceClient: kubeClient.Kube().CoreV1(),
		queue:   queue.NewQueue(time.Second),
		clusterLocalHosts: clusterLocalHosts,
	}

	serviceExportController.serviceInformer = kubeClient.KubeInformer().Core().V1().Services().Informer()
	serviceExportController.serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			serviceExportController.queue.Push(func() error {
				serviceObj, err := convertToService(obj)
				if err != nil {
					return err
				}
				return serviceExportController.HandleNewService(serviceObj)
			})
		},
		DeleteFunc: func(obj interface{}) {
			serviceExportController.queue.Push(func() error {
				serviceObj, err := convertToService(obj)
				if err != nil {
					return err
				}
				return serviceExportController.HandleDeletedService(serviceObj)
			})
		},
	})

	return serviceExportController, nil
}

func (sc *ServiceExportController) Run(stopCh <-chan struct{}) {
	log.Infof("Syncing existing services and serviceexports...")
	sc.doInitialFullSync()
	log.Infof("serviceexport sync complete")
	cache.WaitForCacheSync(stopCh, sc.serviceInformer.HasSynced)
	log.Infof("ServiceExport controller started")
	go sc.queue.Run(stopCh)
}


func (sc *ServiceExportController) HandleNewService(obj *v1.Service) error {
	if sc.isServiceClusterLocal(obj) {
		return nil //don't don anything for marked clusterlocal services
	}
	return sc.createServiceExportIfNotPresent(obj)
}

func (sc *ServiceExportController) HandleDeletedService(obj *v1.Service) error {
	return sc.deleteServiceExportIfPresent(obj)
}

func convertToService(obj interface{}) (*v1.Service, error) {
	cm, ok := obj.(*v1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return nil, fmt.Errorf("couldn't get object from tombstone %#v", obj)
		}
		cm, ok = tombstone.Obj.(*v1.Service)
		if !ok {
			return nil, fmt.Errorf("tombstone contained object that is not a Service %#v", obj)
		}
	}
	return cm, nil
}

func (sc *ServiceExportController) isServiceClusterLocal(service *v1.Service) bool {
	for _, host := range sc.clusterLocalHosts {
		hostComponents := strings.Split(host, ".")
		//name match
		if (hostComponents[0] == "*" || hostComponents[0] == service.Name) && (hostComponents[1] == "*" || hostComponents[1] == service.Namespace) {
			return true
		}
	}
	return false
}

func (sc *ServiceExportController) createServiceExportIfNotPresent(service *v1.Service) error {
	serviceExport := v1alpha1.ServiceExport{}
	serviceExport.Namespace = service.Namespace
	serviceExport.Name = service.Name

	_, err := sc.client.MulticlusterV1alpha1().ServiceExports(service.Namespace).Create(context.TODO(), &serviceExport, metav1.CreateOptions{})

	if err != nil && strings.Contains(err.Error(), "whatever") {
		err = nil //This is the error thrown by the client if there is already an object with the name in the namespace. If that's true, we do nothing
	}
	return err
}

func (sc *ServiceExportController) deleteServiceExportIfPresent(service *v1.Service) error {
	//cannot use the auto-generated client as it hardcodes the namespace in the client struct, and we can't have one client per watched ns
	err := sc.client.MulticlusterV1alpha1().ServiceExports(service.Namespace).Delete(context.TODO(), service.Name, metav1.DeleteOptions{})

	if err != nil && strings.Contains(err.Error(), "not found") {
		err = nil //If it's already gone, then we're happy
	}
	return err
}

func (sc *ServiceExportController) doInitialFullSync() {
	allServices, err := sc.serviceClient.Services("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Errorf("Failed getting services for serviceexport sync. Err=%v", err)
	}
	for _, service := range allServices.Items {
		err = sc.HandleNewService(&service)
		if err != nil {
			log.Errorf("Failed to create serviceexport for service %v in namespace %v Err=%v", service.Name, service.Namespace, err)
		}

	}
}