package plugins

import (
	"context"
	"errors"
	"fmt"
	"github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/sets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CiliumFactory struct {
}

func (c *CiliumFactory) Create(ctx context.Context, kubeClient kube.Client) (*CiliumPlugin, error) {
	// Check if cilium is installed
	resp, err := kubeClient.Ext().ApiextensionsV1beta1().CustomResourceDefinitions().Get(ctx, "ciliumlocalredirectpolicies.cilium.io", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("cilium CRD for local redirect policies not found")
	}
	return &CiliumPlugin{
		ctx:        ctx,
		kubeClient: kubeClient,
	}, nil
}

type CiliumPlugin struct {
	ctx             context.Context
	kubeClient      kube.Client
	ztunnelEndpoint string
	enrolledPods    sets.Set[string]
	policies        map[string]v2.CiliumLocalRedirectPolicy
}

func (c *CiliumPlugin) UpdateHostIP(hostIps []string) error {
	//TODO implement me
	panic("implement me")
}

func (c *CiliumPlugin) UpdateNodeProxy(pod *corev1.Pod, dns bool) {
	// TODO implement me
	panic("implement me")
}

func (c *CiliumPlugin) DumpEnrolledIPs() sets.Set[string] {
	return c.enrolledPods
}

func (c *CiliumPlugin) DelPodOnNode(ip string) error {
	if !c.enrolledPods.Contains(ip) {
		return fmt.Errorf("pod %s not enrolled", ip)
	}
	res := c.kubeClient.Ext().ApiextensionsV1beta1().RESTClient().Delete().Body(c.policies[ip]).Do(c.ctx)
	if res.Error() != nil {
		return res.Error()
	}
	// Remove from cache
	c.enrolledPods.Delete(ip)
	delete(c.policies, ip)
	return nil
}

func (c *CiliumPlugin) UpdatePodOnNode(pod *corev1.Pod) error {
	//TODO implement me
	panic("implement me")
}

func (c *CiliumPlugin) DelZTunnel() error {
	return c.CleanupPodsOnNode()
}

func (c *CiliumPlugin) CleanupPodsOnNode() error {
	var err error
	for _, policy := range c.policies {
		res := c.kubeClient.Ext().ApiextensionsV1beta1().RESTClient().Delete().Body(policy).Do(c.ctx)
		if res.Error() != nil {
			err = errors.Join(err, res.Error())
		}
	}
	c.enrolledPods = make(sets.Set[string])
	c.policies = make(map[string]v2.CiliumLocalRedirectPolicy)
	return err
}
