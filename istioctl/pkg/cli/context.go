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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
)

type Context interface {
	// CLIClient returns a client for the default revision
	CLIClient() (kube.CLIClient, error)
	// CLIClientWithRevision returns a client for the given revision
	CLIClientWithRevision(rev string) (kube.CLIClient, error)
	// InferPodInfoFromTypedResource returns the pod name and namespace for the given typed resource
	InferPodInfoFromTypedResource(name, namespace string) (pod string, ns string, err error)
	// InferPodsFromTypedResource returns the pod names and namespace for the given typed resource
	InferPodsFromTypedResource(name, namespace string) ([]string, string, error)
	// Namespace returns the namespace specified by the user
	Namespace() string
	// IstioNamespace returns the Istio namespace specified by the user
	IstioNamespace() string
	// NamespaceOrDefault returns the namespace specified by the user, or the default namespace if none was specified
	NamespaceOrDefault(namespace string) string
	// ConfigureDefaultNamespace sets the default namespace to use for commands that don't specify a namespace.
	// This should be called before NamespaceOrDefault is called.
	ConfigureDefaultNamespace()
}

type instance struct {
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

func NewCLIContext(rootFlags *RootFlags) Context {
	if rootFlags == nil {
		rootFlags = &RootFlags{
			kubeconfig:       ptr.Of[string](""),
			configContext:    ptr.Of[string](""),
			namespace:        ptr.Of[string](""),
			istioNamespace:   ptr.Of[string](""),
			defaultNamespace: "",
		}
	}
	return &instance{
		RootFlags: *rootFlags,
	}
}

func (i *instance) CLIClientWithRevision(rev string) (kube.CLIClient, error) {
	if i.clients == nil {
		i.clients = make(map[string]kube.CLIClient)
	}
	if i.clients[rev] == nil {
		client, err := newKubeClientWithRevision(i.KubeConfig(), i.KubeContext(), rev)
		if err != nil {
			return nil, err
		}
		i.clients[rev] = client
	}
	return i.clients[rev], nil
}

func (i *instance) CLIClient() (kube.CLIClient, error) {
	return i.CLIClientWithRevision("")
}

func (i *instance) InferPodInfoFromTypedResource(name, namespace string) (pod string, ns string, err error) {
	client, err := i.CLIClient()
	if err != nil {
		return "", "", err
	}
	return handlers.InferPodInfoFromTypedResource(name, i.NamespaceOrDefault(namespace), MakeKubeFactory(client))
}

func (i *instance) InferPodsFromTypedResource(name, namespace string) ([]string, string, error) {
	client, err := i.CLIClient()
	if err != nil {
		return nil, "", err
	}
	return handlers.InferPodsFromTypedResource(name, i.NamespaceOrDefault(namespace), MakeKubeFactory(client))
}

func (i *instance) NamespaceOrDefault(namespace string) string {
	return handleNamespace(namespace, i.DefaultNamespace())
}

// handleNamespace returns the defaultNamespace if the namespace is empty
func handleNamespace(ns, defaultNamespace string) string {
	if ns == corev1.NamespaceAll {
		ns = defaultNamespace
	}
	return ns
}

type fakeInstance struct {
	// clients are cached clients for each revision
	clients   map[string]kube.CLIClient
	rootFlags *RootFlags
	results   map[string][]byte
}

func (f *fakeInstance) CLIClientWithRevision(rev string) (kube.CLIClient, error) {
	if _, ok := f.clients[rev]; !ok {
		cliclient := kube.NewFakeClient()
		if rev != "" {
			kube.SetRevisionForTest(cliclient, rev)
		}
		c := MockClient{
			CLIClient: cliclient,
			Results:   f.results,
		}
		f.clients[rev] = c
	}
	return f.clients[rev], nil
}

func (f *fakeInstance) CLIClient() (kube.CLIClient, error) {
	return f.CLIClientWithRevision("")
}

func (f *fakeInstance) InferPodInfoFromTypedResource(name, namespace string) (pod string, ns string, err error) {
	client, err := f.CLIClient()
	if err != nil {
		return "", "", err
	}
	return handlers.InferPodInfoFromTypedResource(name, f.NamespaceOrDefault(namespace), MakeKubeFactory(client))
}

func (f *fakeInstance) InferPodsFromTypedResource(name, namespace string) ([]string, string, error) {
	client, err := f.CLIClient()
	if err != nil {
		return nil, "", err
	}
	return handlers.InferPodsFromTypedResource(name, f.NamespaceOrDefault(namespace), MakeKubeFactory(client))
}

func (f *fakeInstance) NamespaceOrDefault(namespace string) string {
	return handleNamespace(namespace, f.rootFlags.defaultNamespace)
}

func (f *fakeInstance) KubeConfig() string {
	return ""
}

func (f *fakeInstance) KubeContext() string {
	return ""
}

func (f *fakeInstance) Namespace() string {
	return f.rootFlags.Namespace()
}

func (f *fakeInstance) IstioNamespace() string {
	return f.rootFlags.IstioNamespace()
}

func (f *fakeInstance) ConfigureDefaultNamespace() {
}

type NewFakeContextOption struct {
	Namespace      string
	IstioNamespace string
	Results        map[string][]byte
}

func NewFakeContext(opts *NewFakeContextOption) Context {
	if opts == nil {
		opts = &NewFakeContextOption{}
	}
	ns := opts.Namespace
	ins := opts.IstioNamespace
	return &fakeInstance{
		clients: map[string]kube.CLIClient{},
		rootFlags: &RootFlags{
			kubeconfig:       ptr.Of[string](""),
			configContext:    ptr.Of[string](""),
			namespace:        &ns,
			istioNamespace:   &ins,
			defaultNamespace: "",
		},
		results: opts.Results,
	}
}
