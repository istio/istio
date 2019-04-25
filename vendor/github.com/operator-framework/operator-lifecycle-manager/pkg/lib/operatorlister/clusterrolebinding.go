package operatorlister

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	rbacv1 "k8s.io/client-go/listers/rbac/v1"
)

type UnionClusterRoleBindingLister struct {
	clusterRoleBindingLister rbacv1.ClusterRoleBindingLister
	clusterRoleBindingLock   sync.RWMutex
}

// List lists all ClusterRoleBindings in the indexer.
func (ucl *UnionClusterRoleBindingLister) List(selector labels.Selector) (ret []*v1.ClusterRoleBinding, err error) {
	ucl.clusterRoleBindingLock.RLock()
	defer ucl.clusterRoleBindingLock.RUnlock()

	if ucl.clusterRoleBindingLister == nil {
		return nil, fmt.Errorf("no clusterRoleBinding lister registered")
	}
	return ucl.clusterRoleBindingLister.List(selector)
}

func (ucl *UnionClusterRoleBindingLister) Get(name string) (*v1.ClusterRoleBinding, error) {
	ucl.clusterRoleBindingLock.RLock()
	defer ucl.clusterRoleBindingLock.RUnlock()

	if ucl.clusterRoleBindingLister == nil {
		return nil, fmt.Errorf("no clusterRoleBinding lister registered")
	}
	return ucl.clusterRoleBindingLister.Get(name)
}

func (ucl *UnionClusterRoleBindingLister) RegisterClusterRoleBindingLister(lister rbacv1.ClusterRoleBindingLister) {
	ucl.clusterRoleBindingLock.Lock()
	defer ucl.clusterRoleBindingLock.Unlock()

	ucl.clusterRoleBindingLister = lister
}

func (l *rbacV1Lister) RegisterClusterRoleBindingLister(lister rbacv1.ClusterRoleBindingLister) {
	l.clusterRoleBindingLister.RegisterClusterRoleBindingLister(lister)
}

func (l *rbacV1Lister) ClusterRoleBindingLister() rbacv1.ClusterRoleBindingLister {
	return l.clusterRoleBindingLister
}
