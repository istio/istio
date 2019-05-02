package operatorlister

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	v1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	aregv1 "k8s.io/kube-aggregator/pkg/client/listers/apiregistration/v1"
)

// UnionAPIServiceLister is a custom implementation of an APIService lister that allows a new
// Lister to be registered on the fly
type UnionAPIServiceLister struct {
	apiServiceLister aregv1.APIServiceLister
	apiServiceLock   sync.RWMutex
}

// List lists all APIServices in the indexer.
func (ual *UnionAPIServiceLister) List(selector labels.Selector) (ret []*v1.APIService, err error) {
	ual.apiServiceLock.RLock()
	defer ual.apiServiceLock.RUnlock()

	if ual.apiServiceLister == nil {
		return nil, fmt.Errorf("no apiService lister registered")
	}
	return ual.apiServiceLister.List(selector)
}

// Get retrieves the APIService with the given name
func (ual *UnionAPIServiceLister) Get(name string) (*v1.APIService, error) {
	ual.apiServiceLock.RLock()
	defer ual.apiServiceLock.RUnlock()

	if ual.apiServiceLister == nil {
		return nil, fmt.Errorf("no apiService lister registered")
	}
	return ual.apiServiceLister.Get(name)
}

// RegisterAPIServiceLister registers a new APIServiceLister
func (ual *UnionAPIServiceLister) RegisterAPIServiceLister(lister aregv1.APIServiceLister) {
	ual.apiServiceLock.Lock()
	defer ual.apiServiceLock.Unlock()

	ual.apiServiceLister = lister
}

func (l *apiRegistrationV1Lister) RegisterAPIServiceLister(lister aregv1.APIServiceLister) {
	l.apiServiceLister.RegisterAPIServiceLister(lister)
}

func (l *apiRegistrationV1Lister) APIServiceLister() aregv1.APIServiceLister {
	return l.apiServiceLister
}
