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

type UnionConfigMapLister struct {
	configMapListers map[string]corev1.ConfigMapLister
	configMapLock    sync.RWMutex
}

// List lists all ConfigMaps in the indexer.
func (usl *UnionConfigMapLister) List(selector labels.Selector) (ret []*v1.ConfigMap, err error) {
	usl.configMapLock.RLock()
	defer usl.configMapLock.RUnlock()

	set := make(map[types.UID]*v1.ConfigMap)
	for _, sl := range usl.configMapListers {
		configMaps, err := sl.List(selector)
		if err != nil {
			return nil, err
		}

		for _, configMap := range configMaps {
			set[configMap.GetUID()] = configMap
		}
	}

	for _, configMap := range set {
		ret = append(ret, configMap)
	}

	return
}

// ConfigMaps returns an object that can list and get ConfigMaps.
func (usl *UnionConfigMapLister) ConfigMaps(namespace string) corev1.ConfigMapNamespaceLister {
	usl.configMapLock.RLock()
	defer usl.configMapLock.RUnlock()

	// Check for specific namespace listers
	if sl, ok := usl.configMapListers[namespace]; ok {
		return sl.ConfigMaps(namespace)
	}

	// Check for any namespace-all listers
	if sl, ok := usl.configMapListers[metav1.NamespaceAll]; ok {
		return sl.ConfigMaps(namespace)
	}

	return &NullConfigMapNamespaceLister{}
}

func (usl *UnionConfigMapLister) RegisterConfigMapLister(namespace string, lister corev1.ConfigMapLister) {
	usl.configMapLock.Lock()
	defer usl.configMapLock.Unlock()

	if usl.configMapListers == nil {
		usl.configMapListers = make(map[string]corev1.ConfigMapLister)
	}
	usl.configMapListers[namespace] = lister
}

func (l *coreV1Lister) RegisterConfigMapLister(namespace string, lister corev1.ConfigMapLister) {
	l.configMapLister.RegisterConfigMapLister(namespace, lister)
}

func (l *coreV1Lister) ConfigMapLister() corev1.ConfigMapLister {
	return l.configMapLister
}

// NullConfigMapNamespaceLister is an implementation of a null ConfigMapNamespaceLister. It is
// used to prevent nil pointers when no ConfigMapNamespaceLister has been registered for a given
// namespace.
type NullConfigMapNamespaceLister struct {
	corev1.ConfigMapNamespaceLister
}

// List returns nil and an error explaining that this is a NullConfigMapNamespaceLister.
func (n *NullConfigMapNamespaceLister) List(selector labels.Selector) (ret []*v1.ConfigMap, err error) {
	return nil, fmt.Errorf("cannot list ConfigMaps with a NullConfigMapNamespaceLister")
}

// Get returns nil and an error explaining that this is a NullConfigMapNamespaceLister.
func (n *NullConfigMapNamespaceLister) Get(name string) (*v1.ConfigMap, error) {
	return nil, fmt.Errorf("cannot get ConfigMap with a NullConfigMapNamespaceLister")
}
