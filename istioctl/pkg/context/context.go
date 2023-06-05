// Copyright Istio Authors
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

package context

import (
	"istio.io/istio/istioctl/pkg/option"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/kube"
	"k8s.io/client-go/rest"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type CLIContext struct {
	// clients are cached clients for each revision
	clients map[string]kube.CLIClient

	factory cmdutil.Factory

	option.RootFlags
}

func newKubeClientWithRevision(kubeconfig, configContext, revision string) (kube.CLIClient, error) {
	rc, err := kube.DefaultRestConfig(kubeconfig, configContext, func(config *rest.Config) {
		// We are running a one-off command locally, so we don't need to worry too much about rate limiting
		// Bumping this up greatly decreases install time
		config.QPS = 50
		config.Burst = 100
	})
	if err != nil {
		return nil, err
	}
	return kube.NewCLIClient(kube.NewClientConfigForRestConfig(rc), revision)
}

func NewCLIContext(rootFlags option.RootFlags) *CLIContext {
	return &CLIContext{
		RootFlags: rootFlags,
	}
}

func (c *CLIContext) CLIClientWithRevision(rev string) (kube.CLIClient, error) {
	if c.clients == nil {
		c.clients = make(map[string]kube.CLIClient)
	}
	if rev == "default" {
		rev = ""
	}
	if c.clients[rev] == nil {
		client, err := newKubeClientWithRevision(c.KubeConfig(), c.KubeContext(), rev)
		if err != nil {
			return nil, err
		}
		c.clients[rev] = client
	}
	return c.clients[rev], nil
}

func (c *CLIContext) CLIClient() (kube.CLIClient, error) {
	return c.CLIClientWithRevision("")
}

func (c *CLIContext) RestConfig() (*rest.Config, error) {
	return kube.BuildClientConfig(c.KubeConfig(), c.KubeContext())
}

func (c *CLIContext) InferPodInfoFromTypedResource(arg string) (pod string, ns string, err error) {
	if c.factory == nil {
		client, err := c.CLIClient()
		if err != nil {
			return "", "", err
		}
		c.factory = MakeKubeFactory(client)
	}
	return handlers.InferPodInfoFromTypedResource(arg, handlers.HandleNamespace(c.Namespace(), c.DefaultNamespace()), c.factory)
}
