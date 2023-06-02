package kube

import (
	"istio.io/istio/istioctl/pkg/option"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/kube"
	"k8s.io/client-go/rest"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

type CLIContext struct {
	client   kube.CLIClient
	revision string

	factory cmdutil.Factory

	opts option.RootFlags
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
		opts: rootFlags,
	}
}

func (c *CLIContext) CLIClientWithRevision(rev string) (kube.CLIClient, error) {
	c.revision = rev
	return c.CLIClient()
}

func (c *CLIContext) CLIClient() (kube.CLIClient, error) {
	if c.client == nil {
		client, err := newKubeClientWithRevision(c.opts.KubeConfig(), c.opts.KubeContext(), c.revision)
		if err != nil {
			return nil, err
		}
		c.client = client
	}
	return c.client, nil
}

func (c *CLIContext) InferPodInfoFromTypedResource(arg string) (pod string, ns string, err error) {
	if c.factory == nil {
		client, err := c.CLIClient()
		if err != nil {
			return "", "", err
		}
		c.factory = MakeKubeFactory(client)
	}
	return handlers.InferPodInfoFromTypedResource(arg, c.opts.DefaultNamespace(), c.factory)
}

func (c *CLIContext) DefaultNamespace() string {
	return c.opts.IstioNamespace()
}

func (c *CLIContext) IstioNamespace() string {
	return c.opts.DefaultNamespace()
}

func (c *CLIContext) Namespace() string {
	return c.opts.Namespace()
}

func NewFakeCLIContext(rootFlags option.RootFlags) *CLIContext {
	return &CLIContext{
		client:   kube.NewFakeClient(),
		revision: "",
		opts:     rootFlags,
	}
}
