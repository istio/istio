/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mason

import (
	"fmt"
	"sync"

	"k8s.io/test-infra/boskos/common"

	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
)

// Client extends boskos client with support with resource with leased resources
type Client struct {
	basic     boskosClient
	resources map[string]common.Resource
	lock      sync.RWMutex
}

// NewClient creates a new client from a boskosClient interface
func NewClient(boskosClient boskosClient) *Client {
	return &Client{
		basic:     boskosClient,
		resources: map[string]common.Resource{},
	}
}

// Acquire gets a resource with associated leased resources
func (c *Client) Acquire(rtype, state, dest string) (*common.Resource, error) {
	var resourcesToRelease []common.Resource
	releaseOnFailure := func() {
		for _, r := range resourcesToRelease {
			if err := c.basic.ReleaseOne(r.Name, common.Dirty); err != nil {
				logrus.WithError(err).Warningf("failed to release resource %s", r.Name)
			}
		}
	}
	res, err := c.basic.Acquire(rtype, state, dest)
	if err != nil {
		return nil, err
	}
	var leasedResources common.LeasedResources
	if err = res.UserData.Extract(LeasedResources, &leasedResources); err != nil {
		if _, ok := err.(*common.UserDataNotFound); !ok {
			logrus.WithError(err).Errorf("cannot parse %s from User Data", LeasedResources)
			return nil, err
		}
	}
	resourcesToRelease = append(resourcesToRelease, *res)
	resources, err := c.basic.AcquireByState(res.Name, dest, leasedResources)
	if err != nil {
		releaseOnFailure()
		return nil, err
	}
	resourcesToRelease = append(resourcesToRelease, resources...)
	c.updateResource(*res)
	return res, nil
}

// ReleaseOne will release a resource as well as leased resources associated to it
func (c *Client) ReleaseOne(name, dest string) (allErrors error) {
	res, err := c.getResource(name)
	if err != nil {
		allErrors = err
		return
	}
	resourceNames := []string{name}
	var leasedResources common.LeasedResources
	if err := res.UserData.Extract(LeasedResources, &leasedResources); err != nil {
		if _, ok := err.(*common.UserDataNotFound); !ok {
			logrus.WithError(err).Errorf("cannot parse %s from User Data", LeasedResources)
			allErrors = multierror.Append(allErrors, err)
			if err := c.basic.ReleaseOne(name, dest); err != nil {
				logrus.WithError(err).Warningf("failed to release resource %s", name)
				allErrors = multierror.Append(allErrors, err)
			}
			return
		}
	}
	resourceNames = append(resourceNames, leasedResources...)
	for _, n := range resourceNames {
		if err := c.basic.ReleaseOne(n, dest); err != nil {
			logrus.WithError(err).Warningf("failed to release resource %s", n)
			allErrors = multierror.Append(allErrors, err)
		}
	}
	c.deleteResource(name)
	return
}

// UpdateAll updates all the acquired resources with a given state
func (c *Client) UpdateAll(state string) error {
	return c.basic.UpdateAll(state)
}

func (c *Client) updateResource(r common.Resource) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.resources[r.Name] = r
}

func (c *Client) getResource(name string) (*common.Resource, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	res, ok := c.resources[name]
	if !ok {
		return nil, fmt.Errorf("resource %s not found", name)
	}
	return &res, nil
}

func (c *Client) deleteResource(name string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.resources, name)
}
