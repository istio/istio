package operatorlister

import (
	"fmt"
	"sync"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	corev1 "k8s.io/client-go/listers/core/v1"
)

type UnionPodLister struct {
	podListers map[string]corev1.PodLister
	podLock    sync.RWMutex
}

// List lists all Pods in the indexer.
func (usl *UnionPodLister) List(selector labels.Selector) (ret []*v1.Pod, err error) {
	usl.podLock.RLock()
	defer usl.podLock.RUnlock()

	set := make(map[types.UID]*v1.Pod)
	for _, sl := range usl.podListers {
		pods, err := sl.List(selector)
		if err != nil {
			return nil, err
		}

		for _, pod := range pods {
			set[pod.GetUID()] = pod
		}
	}

	for _, pod := range set {
		ret = append(ret, pod)
	}

	return
}

// Pods returns an object that can list and get Pods.
func (usl *UnionPodLister) Pods(namespace string) corev1.PodNamespaceLister {
	usl.podLock.RLock()
	defer usl.podLock.RUnlock()

	// Check for specific namespace listers
	if sl, ok := usl.podListers[namespace]; ok {
		return sl.Pods(namespace)
	}

	// Check for any namespace-all listers
	if sl, ok := usl.podListers[metav1.NamespaceAll]; ok {
		return sl.Pods(namespace)
	}

	return &NullPodNamespaceLister{}
}

func (usl *UnionPodLister) RegisterPodLister(namespace string, lister corev1.PodLister) {
	usl.podLock.Lock()
	defer usl.podLock.Unlock()

	if usl.podListers == nil {
		usl.podListers = make(map[string]corev1.PodLister)
	}
	usl.podListers[namespace] = lister
}

func (l *coreV1Lister) RegisterPodLister(namespace string, lister corev1.PodLister) {
	l.podLister.RegisterPodLister(namespace, lister)
}

func (l *coreV1Lister) PodLister() corev1.PodLister {
	return l.podLister
}

// NullPodNamespaceLister is an implementation of a null PodNamespaceLister. It is
// used to prevent nil pointers when no PodNamespaceLister has been registered for a given
// namespace.
type NullPodNamespaceLister struct {
	corev1.PodNamespaceLister
}

// List returns nil and an error explaining that this is a NullPodNamespaceLister.
func (n *NullPodNamespaceLister) List(selector labels.Selector) (ret []*v1.Pod, err error) {
	return nil, fmt.Errorf("cannot list Pods with a NullPodNamespaceLister")
}

// Get returns nil and an error explaining that this is a NullPodNamespaceLister.
func (n *NullPodNamespaceLister) Get(name string) (*v1.Pod, error) {
	return nil, fmt.Errorf("cannot get Pod with a NullPodNamespaceLister")
}
