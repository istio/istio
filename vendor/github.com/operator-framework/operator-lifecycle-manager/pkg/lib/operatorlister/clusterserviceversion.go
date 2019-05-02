package operatorlister

import (
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	listers "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/listers/operators/v1alpha1"
)

type UnionClusterServiceVersionLister struct {
	csvListers map[string]listers.ClusterServiceVersionLister
	csvLock    sync.RWMutex
}

// List lists all ClusterServiceVersions in the indexer.
func (ucl *UnionClusterServiceVersionLister) List(selector labels.Selector) (ret []*v1alpha1.ClusterServiceVersion, err error) {
	ucl.csvLock.RLock()
	defer ucl.csvLock.RUnlock()

	set := make(map[types.UID]*v1alpha1.ClusterServiceVersion)
	for _, cl := range ucl.csvListers {
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

// ClusterServiceVersions returns an object that can list and get ClusterServiceVersions.
func (ucl *UnionClusterServiceVersionLister) ClusterServiceVersions(namespace string) listers.ClusterServiceVersionNamespaceLister {
	ucl.csvLock.RLock()
	defer ucl.csvLock.RUnlock()

	// Check for specific namespace listers
	if cl, ok := ucl.csvListers[namespace]; ok {
		return cl.ClusterServiceVersions(namespace)
	}

	// Check for any namespace-all listers
	if cl, ok := ucl.csvListers[metav1.NamespaceAll]; ok {
		return cl.ClusterServiceVersions(namespace)
	}

	return &NullClusterServiceVersionNamespaceLister{}
}

func (ucl *UnionClusterServiceVersionLister) RegisterClusterServiceVersionLister(namespace string, lister listers.ClusterServiceVersionLister) {
	ucl.csvLock.Lock()
	defer ucl.csvLock.Unlock()

	if ucl.csvListers == nil {
		ucl.csvListers = make(map[string]listers.ClusterServiceVersionLister)
	}

	ucl.csvListers[namespace] = lister
}

func (l *operatorsV1alpha1Lister) RegisterClusterServiceVersionLister(namespace string, lister listers.ClusterServiceVersionLister) {
	l.clusterServiceVersionLister.RegisterClusterServiceVersionLister(namespace, lister)
}

func (l *operatorsV1alpha1Lister) ClusterServiceVersionLister() listers.ClusterServiceVersionLister {
	return l.clusterServiceVersionLister
}

// NullClusterServiceVersionNamespaceLister is an implementation of a null ClusterServiceVersionNamespaceLister. It is
// used to prevent nil pointers when no ClusterServiceVersionNamespaceLister has been registered for a given
// namespace.
type NullClusterServiceVersionNamespaceLister struct {
	listers.ClusterServiceVersionNamespaceLister
}

// List returns nil and an error explaining that this is a NullClusterServiceVersionNamespaceLister.
func (n *NullClusterServiceVersionNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.ClusterServiceVersion, err error) {
	return nil, fmt.Errorf("cannot list ClusterServiceVersions with a NullClusterServiceVersionNamespaceLister")
}

// Get returns nil and an error explaining that this is a NullClusterServiceVersionNamespaceLister.
func (n *NullClusterServiceVersionNamespaceLister) Get(name string) (*v1alpha1.ClusterServiceVersion, error) {
	return nil, fmt.Errorf("cannot get ClusterServiceVersion with a NullClusterServiceVersionNamespaceLister")
}
