package operatorlister

import (
	"fmt"
	"sync"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha2"
	listers "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/listers/operators/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

type UnionOperatorGroupLister struct {
	csvListers map[string]listers.OperatorGroupLister
	csvLock    sync.RWMutex
}

// List lists all OperatorGroups in the indexer.
func (uol *UnionOperatorGroupLister) List(selector labels.Selector) (ret []*v1alpha2.OperatorGroup, err error) {
	uol.csvLock.RLock()
	defer uol.csvLock.RUnlock()

	set := make(map[types.UID]*v1alpha2.OperatorGroup)
	for _, cl := range uol.csvListers {
		csvs, err := cl.List(selector)
		if err != nil {
			return nil, err
		}

		for _, csv := range csvs {
			set[csv.GetUID()] = csv
		}
	}

	for _, csv := range set {
		ret = append(ret, csv)
	}

	return
}

// OperatorGroups returns an object that can list and get OperatorGroups.
func (uol *UnionOperatorGroupLister) OperatorGroups(namespace string) listers.OperatorGroupNamespaceLister {
	uol.csvLock.RLock()
	defer uol.csvLock.RUnlock()

	// Check for specific namespace listers
	if cl, ok := uol.csvListers[namespace]; ok {
		return cl.OperatorGroups(namespace)
	}

	// Check for any namespace-all listers
	if cl, ok := uol.csvListers[metav1.NamespaceAll]; ok {
		return cl.OperatorGroups(namespace)
	}

	return &NullOperatorGroupNamespaceLister{}
}

func (uol *UnionOperatorGroupLister) RegisterOperatorGroupLister(namespace string, lister listers.OperatorGroupLister) {
	uol.csvLock.Lock()
	defer uol.csvLock.Unlock()

	if uol.csvListers == nil {
		uol.csvListers = make(map[string]listers.OperatorGroupLister)
	}

	uol.csvListers[namespace] = lister
}

func (l *operatorsV1alpha2Lister) RegisterOperatorGroupLister(namespace string, lister listers.OperatorGroupLister) {
	l.operatorGroupLister.RegisterOperatorGroupLister(namespace, lister)
}

func (l *operatorsV1alpha2Lister) OperatorGroupLister() listers.OperatorGroupLister {
	return l.operatorGroupLister
}

// NullOperatorGroupNamespaceLister is an implementation of a null OperatorGroupNamespaceLister. It is
// used to prevent nil pointers when no OperatorGroupNamespaceLister has been registered for a given
// namespace.
type NullOperatorGroupNamespaceLister struct {
	listers.OperatorGroupNamespaceLister
}

// List returns nil and an error explaining that this is a NullOperatorGroupNamespaceLister.
func (n *NullOperatorGroupNamespaceLister) List(selector labels.Selector) (ret []*v1alpha2.OperatorGroup, err error) {
	return nil, fmt.Errorf("cannot list OperatorGroups with a NullOperatorGroupNamespaceLister")
}

// Get returns nil and an error explaining that this is a NullOperatorGroupNamespaceLister.
func (n *NullOperatorGroupNamespaceLister) Get(name string) (*v1alpha2.OperatorGroup, error) {
	return nil, fmt.Errorf("cannot get OperatorGroup with a NullOperatorGroupNamespaceLister")
}
