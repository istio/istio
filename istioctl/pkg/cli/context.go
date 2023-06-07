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

package cli

import (
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
)

type Context struct {
	// clients are cached clients for each revision
	clients map[string]kube.CLIClient
	RootFlags
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

func NewCLIContext(rootFlags RootFlags) *Context {
	return &Context{
		RootFlags: rootFlags,
	}
}

func (c *Context) CLIClientWithRevision(rev string) (kube.CLIClient, error) {
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

func (c *Context) CLIClient() (kube.CLIClient, error) {
	return c.CLIClientWithRevision("")
}

func (c *Context) InferPodInfoFromTypedResource(name, namespace string) (pod string, ns string, err error) {
	client, err := c.CLIClient()
	if err != nil {
		return "", "", err
	}
	return handlers.InferPodInfoFromTypedResource(name, c.NamespaceOrDefault(namespace), MakeKubeFactory(client))
}

func (c *Context) InferPodsFromTypedResource(name, namespace string) ([]string, string, error) {
	client, err := c.CLIClient()
	if err != nil {
		return nil, "", err
	}
	return handlers.InferPodsFromTypedResource(name, c.NamespaceOrDefault(namespace), MakeKubeFactory(client))
}

func (c *Context) NamespaceOrDefault(namespace string) string {
	return handleNamespace(namespace, c.DefaultNamespace())
}

// handleNamespace returns the defaultNamespace if the namespace is empty
func handleNamespace(ns, defaultNamespace string) string {
	if ns == corev1.NamespaceAll {
		ns = defaultNamespace
	}
	return ns
}

// TODO(hanxiaop) use interface for context and handle real and fake contexts separately.
func NewFakeContext(namespace, istioNamespace string) *Context {
	ns := namespace
	ins := istioNamespace
	return &Context{
		RootFlags: RootFlags{
			kubeconfig:       pointer.String(""),
			configContext:    pointer.String(""),
			namespace:        &ns,
			istioNamespace:   &ins,
			defaultNamespace: "",
		},
	}
}
