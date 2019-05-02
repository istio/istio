package operatorlister

import (
	"fmt"
	"sync"

	v1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	aextv1beta1 "k8s.io/apiextensions-apiserver/pkg/client/listers/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/labels"
)

// UnionCustomResourceDefinitionLister is a custom implementation of an CustomResourceDefinition lister that allows a new
// Lister to be registered on the fly
type UnionCustomResourceDefinitionLister struct {
	CustomResourceDefinitionLister aextv1beta1.CustomResourceDefinitionLister
	CustomResourceDefinitionLock   sync.RWMutex
}

// List lists all CustomResourceDefinitions in the indexer.
func (ucl *UnionCustomResourceDefinitionLister) List(selector labels.Selector) (ret []*v1beta1.CustomResourceDefinition, err error) {
	ucl.CustomResourceDefinitionLock.RLock()
	defer ucl.CustomResourceDefinitionLock.RUnlock()

	if ucl.CustomResourceDefinitionLister == nil {
		return nil, fmt.Errorf("no CustomResourceDefinition lister registered")
	}
	return ucl.CustomResourceDefinitionLister.List(selector)
}

// Get retrieves the CustomResourceDefinition with the given name
func (ucl *UnionCustomResourceDefinitionLister) Get(name string) (*v1beta1.CustomResourceDefinition, error) {
	ucl.CustomResourceDefinitionLock.RLock()
	defer ucl.CustomResourceDefinitionLock.RUnlock()

	if ucl.CustomResourceDefinitionLister == nil {
		return nil, fmt.Errorf("no CustomResourceDefinition lister registered")
	}
	return ucl.CustomResourceDefinitionLister.Get(name)
}

// RegisterCustomResourceDefinitionLister registers a new CustomResourceDefinitionLister
func (ucl *UnionCustomResourceDefinitionLister) RegisterCustomResourceDefinitionLister(lister aextv1beta1.CustomResourceDefinitionLister) {
	ucl.CustomResourceDefinitionLock.Lock()
	defer ucl.CustomResourceDefinitionLock.Unlock()

	ucl.CustomResourceDefinitionLister = lister
}

func (l *apiExtensionsV1beta1Lister) RegisterCustomResourceDefinitionLister(lister aextv1beta1.CustomResourceDefinitionLister) {
	l.customResourceDefinitionLister.RegisterCustomResourceDefinitionLister(lister)
}

func (l *apiExtensionsV1beta1Lister) CustomResourceDefinitionLister() aextv1beta1.CustomResourceDefinitionLister {
	return l.customResourceDefinitionLister
}
