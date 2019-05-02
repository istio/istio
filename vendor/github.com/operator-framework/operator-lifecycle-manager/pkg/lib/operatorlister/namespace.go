package operatorlister

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1 "k8s.io/client-go/listers/core/v1"
)

type UnionNamespaceLister struct {
	namespaceLister corev1.NamespaceLister
	namespaceLock   sync.RWMutex
}

// List lists all Namespaces in the indexer.
func (unl *UnionNamespaceLister) List(selector labels.Selector) (ret []*v1.Namespace, err error) {
	unl.namespaceLock.RLock()
	defer unl.namespaceLock.RUnlock()

	if unl.namespaceLister == nil {
		return nil, fmt.Errorf("no namespace lister registered")
	}
	return unl.namespaceLister.List(selector)
}

func (unl *UnionNamespaceLister) Get(name string) (*v1.Namespace, error) {
	unl.namespaceLock.RLock()
	defer unl.namespaceLock.RUnlock()

	if unl.namespaceLister == nil {
		return nil, fmt.Errorf("no namespace lister registered")
	}
	return unl.namespaceLister.Get(name)
}

func (unl *UnionNamespaceLister) RegisterNamespaceLister(lister corev1.NamespaceLister) {
	unl.namespaceLock.Lock()
	defer unl.namespaceLock.Unlock()

	unl.namespaceLister = lister
}

func (l *coreV1Lister) RegisterNamespaceLister(lister corev1.NamespaceLister) {
	l.namespaceLister.RegisterNamespaceLister(lister)
}

func (l *coreV1Lister) NamespaceLister() corev1.NamespaceLister {
	return l.namespaceLister
}
