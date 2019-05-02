package operatorclient

import (
	"fmt"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CreateSecret creates the Secret.
func (c *Client) CreateSecret(ig *v1.Secret) (*v1.Secret, error) {
	return c.CoreV1().Secrets(ig.GetNamespace()).Create(ig)
}

// GetSecret returns the existing Secret.
func (c *Client) GetSecret(namespace, name string) (*v1.Secret, error) {
	return c.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
}

// DeleteSecret deletes the Secret.
func (c *Client) DeleteSecret(namespace, name string, options *metav1.DeleteOptions) error {
	return c.CoreV1().Secrets(namespace).Delete(name, options)
}

// UpdateSecret will update the given Secret resource.
func (c *Client) UpdateSecret(secret *v1.Secret) (*v1.Secret, error) {
	glog.V(4).Infof("[UPDATE Secret]: %s", secret.GetName())
	oldSa, err := c.GetSecret(secret.GetNamespace(), secret.GetName())
	if err != nil {
		return nil, err
	}
	patchBytes, err := createPatch(oldSa, secret)
	if err != nil {
		return nil, fmt.Errorf("error creating patch for Secret: %v", err)
	}
	return c.CoreV1().Secrets(secret.GetNamespace()).Patch(secret.GetName(), types.StrategicMergePatchType, patchBytes)
}
