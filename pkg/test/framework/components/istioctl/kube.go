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
	"fmt"
	"strings"

	"istio.io/istio/istioctl/cmd"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/test"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/resource"
)

type kubeComponent struct {
	config  Config
	id      resource.ID
	cluster *kubecluster.Cluster
}

func newKube(ctx resource.Context, config Config) Instance {
	n := &kubeComponent{
		config:  config,
		cluster: ctx.Clusters().GetOrDefault(config.Cluster).(*kubecluster.Cluster),
	}
	n.id = ctx.TrackResource(n)

	return n
}

// ID implements resource.Instance
func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Invoke implements WaitForConfigs
func (c *kubeComponent) WaitForConfigs(defaultNamespace string, configs string) error {
	cfgs, _, err := crd.ParseInputs(configs)
	if err != nil {
		return fmt.Errorf("failed to parse input: %v", err)
	}
	for _, cfg := range cfgs {
		ns := cfg.Namespace
		if ns == "" {
			ns = defaultNamespace
		}
		if _, _, err := c.Invoke([]string{"x", "wait", cfg.GroupVersionKind.Kind, cfg.Name + "." + ns}); err != nil {
			return err
		}
	}
	return nil
}

// Invoke implements Instance
func (c *kubeComponent) Invoke(args []string) (string, string, error) {
	cmdArgs := append([]string{
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
func (c *kubeComponent) InvokeOrFail(t test.Failer, args []string) (string, string) {
	output, stderr, err := c.Invoke(args)
	if err != nil {
		t.Logf("Unwanted exception for 'istioctl %s': %v", strings.Join(args, " "), err)
		t.Logf("Output:\n%v", output)
		t.Logf("Error:\n%v", stderr)
		t.FailNow()
	}
	return output, stderr
}
