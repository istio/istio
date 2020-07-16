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
	"bytes"
	"strings"
	"testing"

	"istio.io/istio/istioctl/cmd"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
)

type kubeComponent struct {
	config  Config
	id      resource.ID
	cluster kube.Cluster
}

func newKube(ctx resource.Context, config Config) Instance {
	n := &kubeComponent{
		config:  config,
		cluster: ctx.Clusters().GetOrDefault(config.Cluster).(kube.Cluster),
	}
	n.id = ctx.TrackResource(n)

	return n
}

// ID implements resource.Instance
func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Invoke implements Instance
func (c *kubeComponent) Invoke(args []string) (string, string, error) {
	var cmdArgs = append([]string{
		"--kubeconfig",
		c.cluster.Filename(),
	}, args...)

	var out bytes.Buffer
	var err bytes.Buffer
	rootCmd := cmd.GetRootCmd(cmdArgs)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&err)
	fErr := rootCmd.Execute()
	return out.String(), err.String(), fErr
}

// InvokeOrFail implements Instance
func (c *kubeComponent) InvokeOrFail(t *testing.T, args []string) (string, string) {
	output, stderr, err := c.Invoke(args)
	if err != nil {
		t.Logf("Unwanted exception for 'istioctl %s': %v", strings.Join(args, " "), err)
		t.Logf("Output:\n%v", output)
		t.Logf("Error:\n%v", stderr)
		t.FailNow()
	}
	return output, stderr
}
