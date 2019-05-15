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
	"bytes"
	"testing"

	"istio.io/istio/istioctl/cmd"

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
	// currently nothing, we might add stuff like OS env settings later
}

// The test code will be linked with istioctl, and never runs a separate istioctl binary
type eitherComponent struct {
	config Config
	id     resource.ID
	ctx    resource.Context
}

// New returns a new instance of "istioctl".
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Native, func() {
		i = newEither(ctx, cfg)
		err = nil
	})
	ctx.Environment().Case(environment.Kube, func() {
		i = newEither(ctx, cfg)
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

func newEither(ctx resource.Context, config Config) Instance {
	n := &eitherComponent{
		ctx:    ctx,
		config: config,
	}
	n.id = ctx.TrackResource(n)

	return n
}

// ID implements resource.Instance
func (c *eitherComponent) ID() resource.ID {
	return c.id
}

// GetDiscoveryAddress gets the discovery address for pilot.
func (c *eitherComponent) Invoke(args []string) (string, error) {
	var out bytes.Buffer
	rootCmd := cmd.GetRootCmd(args)
	rootCmd.SetOutput(&out)
	fErr := rootCmd.Execute()
	return out.String(), fErr
}
