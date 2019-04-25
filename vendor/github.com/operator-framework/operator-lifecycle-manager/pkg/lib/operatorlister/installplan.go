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

type UnionInstallPlanLister struct {
	ipListers map[string]listers.InstallPlanLister
	ipLock    sync.RWMutex
}

// List lists all InstallPlans in the indexer.
func (u *UnionInstallPlanLister) List(selector labels.Selector) (ret []*v1alpha1.InstallPlan, err error) {
	u.ipLock.RLock()
	defer u.ipLock.RUnlock()

	set := make(map[types.UID]*v1alpha1.InstallPlan)
	for _, cl := range u.ipListers {
		ips, err := cl.List(selector)
		if err != nil {
			return nil, err
		}

		for _, ip := range ips {
			set[ip.GetUID()] = ip
		}
	}

	for _, ip := range set {
		ret = append(ret, ip)
	}

	return
}

// InstallPlans returns an object that can list and get InstallPlans.
func (u *UnionInstallPlanLister) InstallPlans(namespace string) listers.InstallPlanNamespaceLister {
	u.ipLock.RLock()
	defer u.ipLock.RUnlock()

	// Check for specific namespace listers
	if cl, ok := u.ipListers[namespace]; ok {
		return cl.InstallPlans(namespace)
	}

	// Check for any namespace-all listers
	if cl, ok := u.ipListers[metav1.NamespaceAll]; ok {
		return cl.InstallPlans(namespace)
	}

	return &NullInstallPlanNamespaceLister{}
}

func (u *UnionInstallPlanLister) RegisterInstallPlanLister(namespace string, lister listers.InstallPlanLister) {
	u.ipLock.Lock()
	defer u.ipLock.Unlock()

	if u.ipListers == nil {
		u.ipListers = make(map[string]listers.InstallPlanLister)
	}

	u.ipListers[namespace] = lister
}

func (l *operatorsV1alpha1Lister) RegisterInstallPlanLister(namespace string, lister listers.InstallPlanLister) {
	l.installPlanLister.RegisterInstallPlanLister(namespace, lister)
}

func (l *operatorsV1alpha1Lister) InstallPlanLister() listers.InstallPlanLister {
	return l.installPlanLister
}

// NullInstallPlanNamespaceLister is an implementation of a null InstallPlanNamespaceLister. It is
// used to prevent nil pointers when no InstallPlanNamespaceLister has been registered for a given
// namespace.
type NullInstallPlanNamespaceLister struct {
	listers.InstallPlanNamespaceLister
}

// List returns nil and an error explaining that this is a NullInstallPlanNamespaceLister.
func (n *NullInstallPlanNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.InstallPlan, err error) {
	return nil, fmt.Errorf("cannot list InstallPlans with a NullInstallPlanNamespaceLister")
}

// Get returns nil and an error explaining that this is a NullInstallPlanNamespaceLister.
func (n *NullInstallPlanNamespaceLister) Get(name string) (*v1alpha1.InstallPlan, error) {
	return nil, fmt.Errorf("cannot get InstallPlan with a NullInstallPlanNamespaceLister")
}
