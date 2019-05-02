package operatorlister

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	appsv1 "k8s.io/client-go/listers/apps/v1"
)

type UnionDeploymentLister struct {
	deploymentListers map[string]appsv1.DeploymentLister
	deploymentLock    sync.RWMutex
}

// List lists all Deployments in the indexer.
func (udl *UnionDeploymentLister) List(selector labels.Selector) (ret []*v1.Deployment, err error) {
	udl.deploymentLock.RLock()
	defer udl.deploymentLock.RUnlock()

	var set map[types.UID]*v1.Deployment
	for _, dl := range udl.deploymentListers {
		deployments, err := dl.List(selector)
		if err != nil {
			return nil, err
		}

		for _, deployment := range deployments {
			set[deployment.GetUID()] = deployment
		}
	}

	for _, deployment := range set {
		ret = append(ret, deployment)
	}

	return
}

// Deployments returns an object that can list and get Deployments.
func (udl *UnionDeploymentLister) Deployments(namespace string) appsv1.DeploymentNamespaceLister {
	udl.deploymentLock.RLock()
	defer udl.deploymentLock.RUnlock()

	// Check for specific namespace listers
	if dl, ok := udl.deploymentListers[namespace]; ok {
		return dl.Deployments(namespace)
	}

	// Check for any namespace-all listers
	if dl, ok := udl.deploymentListers[metav1.NamespaceAll]; ok {
		return dl.Deployments(namespace)
	}

	return &NullDeploymentNamespaceLister{}
}

func (udl *UnionDeploymentLister) GetDeploymentsForReplicaSet(rs *v1.ReplicaSet) ([]*v1.Deployment, error) {
	udl.deploymentLock.RLock()
	defer udl.deploymentLock.RUnlock()

	// Check for specific namespace listers
	if dl, ok := udl.deploymentListers[rs.GetNamespace()]; ok {
		return dl.GetDeploymentsForReplicaSet(rs)
	}

	// Check for any namespace-all listers
	if dl, ok := udl.deploymentListers[metav1.NamespaceAll]; ok {
		return dl.GetDeploymentsForReplicaSet(rs)
	}

	return nil, fmt.Errorf("no listers found for namespace %s", rs.GetNamespace())
}

func (udl *UnionDeploymentLister) RegisterDeploymentLister(namespace string, lister appsv1.DeploymentLister) {
	udl.deploymentLock.Lock()
	defer udl.deploymentLock.Unlock()

	if udl.deploymentListers == nil {
		udl.deploymentListers = make(map[string]appsv1.DeploymentLister)
	}

	udl.deploymentListers[namespace] = lister
}

func (l *appsV1Lister) RegisterDeploymentLister(namespace string, lister appsv1.DeploymentLister) {
	l.deploymentLister.RegisterDeploymentLister(namespace, lister)
}

func (l *appsV1Lister) DeploymentLister() appsv1.DeploymentLister {
	return l.deploymentLister
}

// NullDeploymentNamespaceLister is an implementation of a null DeploymentNamespaceLister. It is
// used to prevent nil pointers when no DeploymentNamespaceLister has been registered for a given
// namespace.
type NullDeploymentNamespaceLister struct {
	appsv1.DeploymentNamespaceLister
}

// List returns nil and an error explaining that this is a NullDeploymentNamespaceLister.
func (n *NullDeploymentNamespaceLister) List(selector labels.Selector) (ret []*v1.Deployment, err error) {
	return nil, fmt.Errorf("cannot list Deployments with a NullDeploymentNamespaceLister")
}

// Get returns nil and an error explaining that this is a NullDeploymentNamespaceLister.
func (n *NullDeploymentNamespaceLister) Get(name string) (*v1.Deployment, error) {
	return nil, fmt.Errorf("cannot get Deployment with a NullDeploymentNamespaceLister")
}

// GetDeploymentsForReplicaSet returns nil and an error explaining that this is a NullDeploymentNamespaceLister
func (n *NullDeploymentNamespaceLister) GetDeploymentsForReplicaSet(rs *v1.ReplicaSet) ([]*v1.Deployment, error) {
	return nil, fmt.Errorf("cannot get Deployments for a ReplicaSet with a NullDeploymentNamespaceLister")
}
