package operatorclient

import (
	"fmt"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CreateServiceAccount creates the serviceAccount.
func (c *Client) CreateServiceAccount(ig *v1.ServiceAccount) (*v1.ServiceAccount, error) {
	return c.CoreV1().ServiceAccounts(ig.GetNamespace()).Create(ig)
}

// GetServiceAccount returns the existing serviceAccount.
func (c *Client) GetServiceAccount(namespace, name string) (*v1.ServiceAccount, error) {
	return c.CoreV1().ServiceAccounts(namespace).Get(name, metav1.GetOptions{})
}

// DeleteServiceAccount deletes the serviceAccount.
func (c *Client) DeleteServiceAccount(namespace, name string, options *metav1.DeleteOptions) error {
	return c.CoreV1().ServiceAccounts(namespace).Delete(name, options)
}

// UpdateServiceAccount will update the given ServiceAccount resource.
func (c *Client) UpdateServiceAccount(sa *v1.ServiceAccount) (*v1.ServiceAccount, error) {
	glog.V(4).Infof("[UPDATE ServiceAccount]: %s", sa.GetName())
	oldSa, err := c.GetServiceAccount(sa.GetNamespace(), sa.GetName())
	if err != nil {
		return nil, err
	}
	patchBytes, err := createPatch(oldSa, sa)
	if err != nil {
		return nil, fmt.Errorf("error creating patch for ServiceAccount: %v", err)
	}
	return c.Core().ServiceAccounts(sa.GetNamespace()).Patch(sa.GetName(), types.StrategicMergePatchType, patchBytes)
}
