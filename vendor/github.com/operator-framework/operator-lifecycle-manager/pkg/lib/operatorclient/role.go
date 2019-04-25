package operatorclient

import (
	"fmt"

	"github.com/golang/glog"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CreateRole creates the role.
func (c *Client) CreateRole(r *rbacv1.Role) (*rbacv1.Role, error) {
	return c.RbacV1().Roles(r.GetNamespace()).Create(r)
}

// GetRole returns the existing role.
func (c *Client) GetRole(namespace, name string) (*rbacv1.Role, error) {
	return c.RbacV1().Roles(namespace).Get(name, metav1.GetOptions{})
}

// DeleteRole deletes the role.
func (c *Client) DeleteRole(namespace, name string, options *metav1.DeleteOptions) error {
	return c.RbacV1().Roles(namespace).Delete(name, options)
}

// UpdateRole will update the given Role resource.
func (c *Client) UpdateRole(crb *rbacv1.Role) (*rbacv1.Role, error) {
	glog.V(4).Infof("[UPDATE Role]: %s", crb.GetName())
	oldCrb, err := c.GetRole(crb.GetNamespace(), crb.GetName())
	if err != nil {
		return nil, err
	}
	patchBytes, err := createPatch(oldCrb, crb)
	if err != nil {
		return nil, fmt.Errorf("error creating patch for Role: %v", err)
	}
	return c.RbacV1().Roles(crb.GetNamespace()).Patch(crb.GetName(), types.StrategicMergePatchType, patchBytes)
}
