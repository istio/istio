package watchers

import (
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	api "k8s.io/api/core/v1"
	cache "k8s.io/client-go/tools/cache"
	"istio.io/istio/pkg/log"
	"k8s.io/apimachinery/pkg/labels"
)

type ServiceUpdate struct {
	Service *api.Service
	Op      Operation
}

type ServiceWatcher struct {
	serviceController cache.Controller
	serviceLister     cache.Indexer
	broadcaster       *Broadcaster
}

type ServiceUpdatesHandler interface {
	OnServiceUpdate(serviceUpdate *ServiceUpdate)
}

func (svcw *ServiceWatcher) addEventHandler(obj interface{}) {
	service, ok := obj.(*api.Service)
	if !ok {
		return
	}
	svcw.broadcaster.Notify(&ServiceUpdate{Op: ADD, Service: service})
}

func (svcw *ServiceWatcher) deleteEventHandler(obj interface{}) {
	service, ok := obj.(*api.Service)
	if !ok {
		return
	}
	svcw.broadcaster.Notify(&ServiceUpdate{Op: REMOVE, Service: service})
}

func (svcw *ServiceWatcher) updateEventHandler(oldObj, newObj interface{}) {
	service, ok := newObj.(*api.Service)
	if !ok {
		return
	}
	if !reflect.DeepEqual(newObj, oldObj) {
		svcw.broadcaster.Notify(&ServiceUpdate{Op: UPDATE, Service: service})
	}
}

func (svcw *ServiceWatcher) RegisterHandler(handler ServiceUpdatesHandler) {
	svcw.broadcaster.Add(ListenerFunc(func(instance interface{}) {
		handler.OnServiceUpdate(instance.(*ServiceUpdate))
	}))
}

func (svcw *ServiceWatcher) List() []*api.Service {
	objList := svcw.serviceLister.List()
	svcInstances := make([]*api.Service, len(objList))
	for i, ins := range objList {
		svcInstances[i] = ins.(*api.Service)
	}
	return svcInstances
}

func (svcw *ServiceWatcher) ListBySelector(set map[string]string) (ret []*api.Service, err error) {
	/*
	objList := svcw.serviceLister.List()
	services := make([]*api.Service, 0)
	for _, ins := range objList {
		service := ins.(*api.Service)

		if IsMapContain(selector, service.Spec.Selector) {
			services = append(services, service)
		}
	}
	return services
	*/
	selector := labels.SelectorFromSet(set)
	err = cache.ListAll(svcw.serviceLister, selector, func(m interface{}) {
		ret = append(ret, m.(*api.Service))
	})
	return ret, err
}

func (svcw *ServiceWatcher) HasSynced() bool {
	return svcw.serviceController.HasSynced()
}

// StartServiceWatcher: start watching updates for services from Kuberentes API server
func StartServiceWatcher(clientset kubernetes.Interface, resyncPeriod time.Duration, stopCh <- chan struct{}) (*ServiceWatcher, error) {
	svcw := ServiceWatcher{}

	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    svcw.addEventHandler,
		DeleteFunc: svcw.deleteEventHandler,
		UpdateFunc: svcw.updateEventHandler,
	}

	svcw.broadcaster = NewBroadcaster()
	lw := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "services", metav1.NamespaceAll, fields.Everything())
	svcw.serviceLister, svcw.serviceController = cache.NewIndexerInformer(
		lw,
		&api.Service{}, resyncPeriod, eventHandler,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	go svcw.serviceController.Run(stopCh)
	return &svcw, nil
}
