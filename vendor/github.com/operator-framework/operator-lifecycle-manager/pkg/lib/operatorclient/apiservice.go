package operatorclient

import (
	"fmt"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
)

// CreateAPIService creates the APIService.
func (c *Client) CreateAPIService(ig *apiregistrationv1.APIService) (*apiregistrationv1.APIService, error) {
	return c.ApiregistrationV1Interface().ApiregistrationV1().APIServices().Create(ig)
}

// GetAPIService returns the existing APIService.
func (c *Client) GetAPIService(name string) (*apiregistrationv1.APIService, error) {
	return c.ApiregistrationV1Interface().ApiregistrationV1().APIServices().Get(name, metav1.GetOptions{})
}

// DeleteAPIService deletes the APIService.
func (c *Client) DeleteAPIService(name string, options *metav1.DeleteOptions) error {
	return c.ApiregistrationV1Interface().ApiregistrationV1().APIServices().Delete(name, options)
}

// UpdateAPIService will update the given APIService resource.
func (c *Client) UpdateAPIService(apiService *apiregistrationv1.APIService) (*apiregistrationv1.APIService, error) {
	glog.V(4).Infof("[UPDATE APIService]: %s", apiService.GetName())
	oldAPIService, err := c.GetAPIService(apiService.GetName())
	if err != nil {
		return nil, err
	}
	patchBytes, err := createPatch(oldAPIService, apiService)
	if err != nil {
		return nil, fmt.Errorf("error creating patch for APIService: %v", err)
	}
	return c.ApiregistrationV1Interface().ApiregistrationV1().APIServices().Patch(apiService.GetName(), types.StrategicMergePatchType, patchBytes)
}
