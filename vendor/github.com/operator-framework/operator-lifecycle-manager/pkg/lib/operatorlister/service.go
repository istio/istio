package operatorlister

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	corev1 "k8s.io/client-go/listers/core/v1"
)

type UnionServiceLister struct {
	serviceListers map[string]corev1.ServiceLister
	serviceLock    sync.RWMutex
}

// List lists all Services in the indexer.
func (usl *UnionServiceLister) List(selector labels.Selector) (ret []*v1.Service, err error) {
	usl.serviceLock.RLock()
	defer usl.serviceLock.RUnlock()

	set := make(map[types.UID]*v1.Service)
	for _, sl := range usl.serviceListers {
		services, err := sl.List(selector)
		if err != nil {
			return nil, err
		}

		for _, service := range services {
			set[service.GetUID()] = service
		}
	}

	for _, service := range set {
		ret = append(ret, service)
	}

	return
}

// Services returns an object that can list and get Services.
func (usl *UnionServiceLister) Services(namespace string) corev1.ServiceNamespaceLister {
	usl.serviceLock.RLock()
	defer usl.serviceLock.RUnlock()

	// Check for specific namespace listers
	if sl, ok := usl.serviceListers[namespace]; ok {
		return sl.Services(namespace)
	}

	// Check for any namespace-all listers
	if sl, ok := usl.serviceListers[metav1.NamespaceAll]; ok {
		return sl.Services(namespace)
	}

	return &NullServiceNamespaceLister{}
}

func (usl *UnionServiceLister) GetPodServices(pod *v1.Pod) ([]*v1.Service, error) {
	usl.serviceLock.RLock()
	defer usl.serviceLock.RUnlock()

	// Check for specific namespace listers
	if sl, ok := usl.serviceListers[pod.GetNamespace()]; ok {
		return sl.GetPodServices(pod)
	}

	// Check for any namespace-all listers
	if sl, ok := usl.serviceListers[metav1.NamespaceAll]; ok {
		return sl.GetPodServices(pod)
	}

	return nil, fmt.Errorf("could not find service lister registered for namspace %s", pod.GetNamespace())
}

func (usl *UnionServiceLister) RegisterServiceLister(namespace string, lister corev1.ServiceLister) {
	usl.serviceLock.Lock()
	defer usl.serviceLock.Unlock()

	if usl.serviceListers == nil {
		usl.serviceListers = make(map[string]corev1.ServiceLister)
	}
	usl.serviceListers[namespace] = lister
}

func (l *coreV1Lister) RegisterServiceLister(namespace string, lister corev1.ServiceLister) {
	l.serviceLister.RegisterServiceLister(namespace, lister)
}

func (l *coreV1Lister) ServiceLister() corev1.ServiceLister {
	return l.serviceLister
}

// NullServiceNamespaceLister is an implementation of a null ServiceNamespaceLister. It is
// used to prevent nil pointers when no ServiceNamespaceLister has been registered for a given
// namespace.
type NullServiceNamespaceLister struct {
	corev1.ServiceNamespaceLister
}

// List returns nil and an error explaining that this is a NullServiceNamespaceLister.
func (n *NullServiceNamespaceLister) List(selector labels.Selector) (ret []*v1.Service, err error) {
	return nil, fmt.Errorf("cannot list Services with a NullServiceNamespaceLister")
}

// Get returns nil and an error explaining that this is a NullServiceNamespaceLister.
func (n *NullServiceNamespaceLister) Get(name string) (*v1.Service, error) {
	return nil, fmt.Errorf("cannot get Service with a NullServiceNamespaceLister")
}

// GetPodServices returns nil and an error explaining that this is a NullServiceNamespaceLister.
func (n *NullServiceNamespaceLister) GetPodServices(pod *v1.Pod) ([]*v1.Service, error) {
	return nil, fmt.Errorf("could not get pod services with a NullServiceNamespaceLister")
}
