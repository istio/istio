//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package istioctl

import (
	"fmt"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance represents "istioctl"
type Instance interface {
	// WaitForConfig will wait until all passed in config has been distributed
	WaitForConfig(defaultNamespace string, configs string) error

	// Invoke invokes an istioctl command and returns the output and exception.
	// stdout and stderr will be returned as different strings
	Invoke(args []string) (string, string, error)

	// InvokeOrFail calls Invoke and fails tests if it returns en err
	InvokeOrFail(t test.Failer, args []string) (string, string)
}

// Config is structured config for the istioctl component
type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster cluster.Cluster

	// IstioNamespace where istio is deployed
	IstioNamespace string
}

// New returns a new instance of "istioctl".
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	return newKube(ctx, cfg)
}

// NewOrFail returns a new instance of "istioctl".
func NewOrFail(t resource.ContextFailer, config Config) Instance {
	i, err := New(t, config)
	if err != nil {
		t.Fatalf("istioctl.NewOrFail:: %v", err)
	}

	return i
}

func (c *Config) String() string {
	result := ""
	result += fmt.Sprintf("Cluster:                      %s\n", c.Cluster)
	return result
}
