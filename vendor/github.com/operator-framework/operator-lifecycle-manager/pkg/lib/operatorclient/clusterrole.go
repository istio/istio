package operatorclient

import (
	"fmt"

	"github.com/golang/glog"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CreateClusterRole creates the ClusterRole.
func (c *Client) CreateClusterRole(r *rbacv1.ClusterRole) (*rbacv1.ClusterRole, error) {
	return c.RbacV1().ClusterRoles().Create(r)
}

// GetClusterRole returns the existing ClusterRole.
func (c *Client) GetClusterRole(name string) (*rbacv1.ClusterRole, error) {
	return c.RbacV1().ClusterRoles().Get(name, metav1.GetOptions{})
}

// DeleteClusterRole deletes the ClusterRole
func (c *Client) DeleteClusterRole(name string, options *metav1.DeleteOptions) error {
	return c.RbacV1().ClusterRoles().Delete(name, options)
}

// UpdateClusterRole will update the given ClusterRole.
func (c *Client) UpdateClusterRole(crb *rbacv1.ClusterRole) (*rbacv1.ClusterRole, error) {
	glog.V(4).Infof("[UPDATE Role]: %s", crb.GetName())
	oldCrb, err := c.GetClusterRole(crb.GetName())
	if err != nil {
		return nil, err
	}
	patchBytes, err := createPatch(oldCrb, crb)
	if err != nil {
		return nil, fmt.Errorf("error creating patch for Role: %v", err)
	}
	return c.RbacV1().ClusterRoles().Patch(crb.GetName(), types.StrategicMergePatchType, patchBytes)
}
