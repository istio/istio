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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
)

type Context interface {
	// CLIClient returns a client for the default revision
	CLIClient() (kube.CLIClient, error)
	// CLIClientWithRevision returns a client for the given revision
	CLIClientWithRevision(rev string) (kube.CLIClient, error)
	// RevisionOrDefault returns the given revision if non-empty, otherwise returns the default revision
	RevisionOrDefault(rev string) string
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
	// CLIClientsForContexts returns clients for all the given contexts in a kubeconfig.
	CLIClientsForContexts(contexts []string) ([]kube.CLIClient, error)
}

type instance struct {
	// clients are cached clients for each revision
	clients map[string]kube.CLIClient
	// remoteClients are cached clients for each context with empty revision.
	remoteClients map[string]kube.CLIClient
	// defaultWatcher watches for changes to the default revision
	defaultWatcher revisions.DefaultWatcher
	RootFlags
}

func newKubeClientWithRevision(
	kubeconfig,
	configContext,
	revision string,
	timeout time.Duration,
	impersonateConfig rest.ImpersonationConfig,
) (kube.CLIClient, error) {
	rc, err := kube.DefaultRestConfig(kubeconfig, configContext, func(config *rest.Config) {
		// We are running a one-off command locally, so we don't need to worry too much about rate limiting
		// Bumping this up greatly decreases install time
		config.QPS = 50
		config.Burst = 100
		config.Impersonate = impersonateConfig
	})
	if err != nil {
		return nil, err
	}
	return kube.NewCLIClient(
		kube.NewClientConfigForRestConfig(rc),
		kube.WithRevision(revision),
		kube.WithCluster(cluster.ID(configContext)),
		kube.WithTimeout(timeout),
	)
}

func NewCLIContext(rootFlags *RootFlags) Context {
	if rootFlags == nil {
		rootFlags = &RootFlags{
			kubeconfig:       ptr.Of[string](""),
			configContext:    ptr.Of[string](""),
			impersonate:      ptr.Of[string](""),
			impersonateUID:   ptr.Of[string](""),
			impersonateGroup: nil,
			namespace:        ptr.Of[string](""),
			istioNamespace:   ptr.Of[string](""),
			defaultNamespace: "",
			kubeTimeout:      ptr.Of[string](""),
		}
	}
	return &instance{
		RootFlags: *rootFlags,
	}
}

func (i *instance) getImpersonateConfig() rest.ImpersonationConfig {
	impersonateConfig := rest.ImpersonationConfig{}
	if len(*i.impersonate) > 0 {
		impersonateConfig.UserName = *i.impersonate
		impersonateConfig.UID = *i.impersonateUID
		impersonateConfig.Groups = *i.impersonateGroup
	}
	return impersonateConfig
}

func (i *instance) CLIClientWithRevision(rev string) (kube.CLIClient, error) {
	if i.clients == nil {
		i.clients = make(map[string]kube.CLIClient)
	}

	timeout, err := i.KubeClientTimeout()
	if err != nil {
		return nil, fmt.Errorf("error parsing kubeclient-timeout: %v", err)
	}

	if i.clients[rev] == nil {
		client, err := newKubeClientWithRevision(*i.kubeconfig, *i.configContext, rev, timeout, i.getImpersonateConfig())
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

func (i *instance) CLIClientsForContexts(contexts []string) ([]kube.CLIClient, error) {
	if i.remoteClients == nil {
		i.remoteClients = make(map[string]kube.CLIClient)
	}
	// Validate if "all" incoming contexts are present in the kubeconfig. Fail fast if not.
	rawConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(kube.ConfigLoadingRules(*i.kubeconfig), nil).RawConfig()
	if err != nil {
		return nil, fmt.Errorf("getting raw kubeconfig from file %q: %v", *i.kubeconfig, err)
	}
	impersonateConfig := i.getImpersonateConfig()
	rawConfigContexts := maps.Keys(rawConfig.Contexts)
	for _, c := range contexts {
		if !slices.Contains(rawConfigContexts, c) {
			return nil, fmt.Errorf("context %q not found", c)
		}
	}

	clientTimeout, err := i.KubeClientTimeout()
	if err != nil {
		return nil, fmt.Errorf("error parsing kubeclient-timeout: %v", err)
	}

	var clients []kube.CLIClient
	for _, contextName := range contexts {
		if i.remoteClients[contextName] != nil {
			clients = append(clients, i.remoteClients[contextName])
			continue
		}

		c, err := newKubeClientWithRevision(*i.kubeconfig, contextName, "", clientTimeout, impersonateConfig)
		if err != nil {
			return nil, fmt.Errorf("creating kube client for context %q: %v", contextName, err)
		}
		clients = append(clients, c)
		i.remoteClients[contextName] = c
	}
	return clients, nil
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

func (i *instance) RevisionOrDefault(rev string) string {
	if rev != "" {
		return rev
	}

	// Try to get the default revision from the default watcher
	// We create a simple approach that doesn't cause circular dependencies

	stop := make(chan struct{})
	defer close(stop)
	if i.defaultWatcher == nil {
		// Try to initialize the default watcher using a basic client
		if basicClient := i.getBasicClientForDefaultWatcher(); basicClient != nil {
			i.defaultWatcher = revisions.NewDefaultWatcher(basicClient, "")
			basicClient.RunAndWait(stop)
			go i.defaultWatcher.Run(stop)
		}
	}

	if i.defaultWatcher != nil {
		if defaultRev := i.defaultWatcher.GetDefault(); defaultRev != "" {
			return defaultRev
		}
		log.Warnf("default revision watcher not synced, falling back to \"default\"")
	}

	return "default"
}

// getBasicClientForDefaultWatcher creates a basic kube client just for watching default revisions
// This avoids circular dependencies by not using RevisionOrDefault
func (i *instance) getBasicClientForDefaultWatcher() kube.Client {
	timeout, err := i.KubeClientTimeout()
	if err != nil {
		return nil
	}

	client, err := newKubeClientWithRevision(*i.kubeconfig, *i.configContext, "", timeout, i.getImpersonateConfig())
	if err != nil {
		return nil
	}

	return client
}

type fakeInstance struct {
	// clients are cached clients for each revision
	clients   map[string]kube.CLIClient
	rootFlags *RootFlags
	results   map[string][]byte
	objects   []runtime.Object
	version   string
}

func (f *fakeInstance) CLIClientWithRevision(rev string) (kube.CLIClient, error) {
	if _, ok := f.clients[rev]; !ok {
		var cliclient kube.CLIClient
		if f.version != "" {
			cliclient = kube.NewFakeClientWithVersion(f.version, f.objects...)
		} else {
			cliclient = kube.NewFakeClient(f.objects...)
		}
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

func (f *fakeInstance) CLIClientsForContexts(contexts []string) ([]kube.CLIClient, error) {
	c, err := f.CLIClientWithRevision("")
	if err != nil {
		return nil, err
	}
	return []kube.CLIClient{c}, nil
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

func (f *fakeInstance) Namespace() string {
	return f.rootFlags.Namespace()
}

func (f *fakeInstance) IstioNamespace() string {
	return f.rootFlags.IstioNamespace()
}

func (f *fakeInstance) RevisionOrDefault(rev string) string {
	// For fake instance, return "default" if empty (consistent with real implementation fallback)
	if rev == "" {
		return "default"
	}
	return rev
}

type NewFakeContextOption struct {
	Namespace      string
	IstioNamespace string
	Results        map[string][]byte
	// Objects are the objects to be applied to the fake client
	Objects []runtime.Object
	// Version is the version of the fake client
	Version string
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
			impersonate:      ptr.Of[string](""),
			impersonateUID:   ptr.Of[string](""),
			impersonateGroup: nil,
			defaultNamespace: "",
		},
		results: opts.Results,
		objects: opts.Objects,
		version: opts.Version,
	}
}
