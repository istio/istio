package operatorlister

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	rbacv1 "k8s.io/client-go/listers/rbac/v1"
)

type UnionClusterRoleLister struct {
	clusterRoleLister rbacv1.ClusterRoleLister
	clusterRoleLock   sync.RWMutex
}

// List lists all ClusterRoles in the indexer.
func (ucl *UnionClusterRoleLister) List(selector labels.Selector) (ret []*v1.ClusterRole, err error) {
	ucl.clusterRoleLock.RLock()
	defer ucl.clusterRoleLock.RUnlock()

	if ucl.clusterRoleLister == nil {
		return nil, fmt.Errorf("no clusterRole lister registered")
	}
	return ucl.clusterRoleLister.List(selector)
}

func (ucl *UnionClusterRoleLister) Get(name string) (*v1.ClusterRole, error) {
	ucl.clusterRoleLock.RLock()
	defer ucl.clusterRoleLock.RUnlock()

	if ucl.clusterRoleLister == nil {
		return nil, fmt.Errorf("no clusterRole lister registered")
	}
	return ucl.clusterRoleLister.Get(name)
}

func (ucl *UnionClusterRoleLister) RegisterClusterRoleLister(lister rbacv1.ClusterRoleLister) {
	ucl.clusterRoleLock.Lock()
	defer ucl.clusterRoleLock.Unlock()

	ucl.clusterRoleLister = lister
}

func (l *rbacV1Lister) RegisterClusterRoleLister(lister rbacv1.ClusterRoleLister) {
	l.clusterRoleLister.RegisterClusterRoleLister(lister)
}

func (l *rbacV1Lister) ClusterRoleLister() rbacv1.ClusterRoleLister {
	return l.clusterRoleLister
}
