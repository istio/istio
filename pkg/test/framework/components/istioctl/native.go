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
	"strings"
	"testing"

	"istio.io/istio/istioctl/cmd"

	"istio.io/istio/pkg/test/framework/resource"
)

// Note: The native tests don't talk to a K8s API server, so most istioctl commands won't work
type nativeComponent struct {
	config Config
	id     resource.ID
	ctx    resource.Context
}

func newNative(ctx resource.Context, config Config) Instance {
	n := &nativeComponent{
		ctx:    ctx,
		config: config,
	}
	n.id = ctx.TrackResource(n)

	return n
}

// ID implements resource.Instance
func (c *nativeComponent) ID() resource.ID {
	return c.id
}

// Invoke implements Instance
func (c *nativeComponent) Invoke(args []string) (string, error) {
	var out bytes.Buffer
	rootCmd := cmd.GetRootCmd(args)
	rootCmd.SetOutput(&out)
	fErr := rootCmd.Execute()
	return out.String(), fErr
}

// InvokeOrFail implements Instance
func (c *nativeComponent) InvokeOrFail(t *testing.T, args []string) string {
	output, err := c.Invoke(args)
	if err != nil {
		t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(args, " "), err)
	}
	return output
}
