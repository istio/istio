//  Copyright 2019 Istio Authors
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
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/resource"
)

type Instance interface {
	// Invoke invokes an istioctl command and returns the output and exception.
	// Cobra commands don't make it easy to separate stdout and stderr and the string parameter
	// will receive both.
	Invoke(args []string) (string, error)
}

// Structured config for the istioctl component
type Config struct {
	// Which KubeConfig should be used in a multicluster environment
	KubeIndex int
}

// New returns a new instance of "istioctl".
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Native, func() {
		i = newNative(ctx, cfg)
		err = nil
	})
	ctx.Environment().Case(environment.Kube, func() {
		i = newKube(ctx, cfg)
		err = nil
	})

	return
}

// NewOrFail returns a new instance of "istioctl".
func NewOrFail(t *testing.T, c resource.Context, config Config) Instance {
	i, err := New(c, config)
	if err != nil {
		t.Fatalf("istioctl.NewOrFail:: %v", err)
	}
	return i
}

func (c *Config) String() string {
	result := ""
	result += fmt.Sprintf("KubeIndex:                      %d\n", c.KubeIndex)
	return result
}
