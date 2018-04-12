// Copyright 2017,2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package framework

import (
	"go.uber.org/multierr"
	"time"

	"istio.io/istio/tests/util"
)

const defaultPropagationDelay = 10 * time.Second

// DeployableConfig is a collection of configs that are applied/deleted as a single unit.
type DeployableConfig struct {
	Namespace  string
	YamlFiles  []string
	applied    []string
	Kubeconfig string
}

// Setup pushes the config and waits for it to propagate to all nodes in the cluster.
func (c *DeployableConfig) Setup() error {
	c.applied = []string{}

	// Apply the configs.
	for _, yamlFile := range c.YamlFiles {
		if err := util.KubeApply(c.Namespace, yamlFile, c.Kubeconfig); err != nil {
			// Run the teardown function now and return
			_ = c.Teardown()
			return err
		}
		c.applied = append(c.applied, yamlFile)
	}

	// Sleep for a while to allow the change to propagate.
	time.Sleep(c.propagationDelay())
	return nil
}

// Teardown deletes the deployed configuration.
func (c *DeployableConfig) Teardown() error {
	err := c.TeardownNoDelay()

	// Sleep for a while to allow the change to propagate.
	time.Sleep(c.propagationDelay())
	return err
}

// Teardown deletes the deployed configuration.
func (c *DeployableConfig) TeardownNoDelay() error {
	var err error
	for _, yamlFile := range c.applied {
		err = multierr.Append(err, util.KubeDelete(c.Namespace, yamlFile, c.Kubeconfig))
	}
	c.applied = []string{}
	return err
}

func (c *DeployableConfig) propagationDelay() time.Duration {
	return defaultPropagationDelay
}
