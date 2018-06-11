// Copyright 2017 Istio Authors.
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

// nolint: lll
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -a mixer/adapter/kubernetesenv/config/config.proto -x "-n kubernetesenv"
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -t mixer/adapter/kubernetesenv/template/template.proto

// Package kubernetesenv provides functionality to adapt mixer behavior to the
// kubernetes environment. Primarily, it is used to generate values as part
// of Mixer's attribute generation preprocessing phase. These values will be
// transformed into attributes that can be used for subsequent config
// resolution and adapter dispatch and execution.
package kubernetesenv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/api/core/v1"
	k8s "k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // needed for auth
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/mixer/adapter/kubernetesenv/config"
	ktmpl "istio.io/istio/mixer/adapter/kubernetesenv/template"
	"istio.io/istio/mixer/pkg/adapter"
)

const (
	// parsing
	kubePrefix = "kubernetes://"

	// k8s cache invalidation
	// TODO: determine a reasonable default
	defaultRefreshPeriod = 5 * time.Minute
)

var (
	conf = &config.Params{
		KubeconfigPath:       "",
		CacheRefreshDuration: defaultRefreshPeriod,
	}
)

type (
	builder struct {
		adapterConfig *config.Params
		newClientFn   clientFactoryFn

		sync.Mutex
		controllers map[string]cacheController
	}

	handler struct {
		k8sCache cacheController
		env      adapter.Env
		params   *config.Params
	}

	// used strictly for testing purposes
	clientFactoryFn func(kubeconfigPath string, env adapter.Env) (k8s.Interface, error)
)

// compile-time validation
var _ ktmpl.Handler = &handler{}
var _ ktmpl.HandlerBuilder = &builder{}

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "kubernetesenv",
		Impl:        "istio.io/istio/mixer/adapter/kubernetesenv",
		Description: "Provides platform specific functionality for the kubernetes environment",
		SupportedTemplates: []string{
			ktmpl.TemplateName,
		},
		DefaultConfig: conf,

		NewBuilder: func() adapter.HandlerBuilder { return newBuilder(newKubernetesClient) },
	}
}

func (b *builder) SetAdapterConfig(c adapter.Config) {
	b.adapterConfig = c.(*config.Params)
}

// Validate is responsible for ensuring that all the configuration state given to the builder is
// correct.
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	return
}

func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	paramsProto := b.adapterConfig
	stopChan := make(chan struct{})
	refresh := paramsProto.CacheRefreshDuration
	path, exists := os.LookupEnv("KUBECONFIG")
	if !exists {
		path = paramsProto.KubeconfigPath
	}

	// only ever build a controller for a config once. this potential blocks
	// the Build() for multiple handlers using the same config until the first
	// one has synced. This should be OK, as the WaitForCacheSync was meant to
	// provide this basic functionality before.
	b.Lock()
	defer b.Unlock()
	controller, found := b.controllers[path]
	if !found {
		clientset, err := b.newClientFn(path, env)
		if err != nil {
			return nil, fmt.Errorf("could not build kubernetes client: %v", err)
		}
		controller = newCacheController(clientset, refresh, paramsProto.IsV18Cluster, env)
		env.ScheduleDaemon(func() { controller.Run(stopChan) })
		// ensure that any request is only handled after
		// a sync has occurred
		env.Logger().Infof("Waiting for kubernetes cache sync...")
		if success := cache.WaitForCacheSync(stopChan, controller.HasSynced); !success {
			stopChan <- struct{}{}
			return nil, errors.New("cache sync failure")
		}
		env.Logger().Infof("Cache sync successful.")
		b.controllers[path] = controller
	}

	return &handler{
		env:      env,
		k8sCache: controller,
		params:   paramsProto,
	}, nil
}

func newBuilder(clientFactory clientFactoryFn) *builder {
	return &builder{
		newClientFn:   clientFactory,
		controllers:   make(map[string]cacheController),
		adapterConfig: conf,
	}
}

func (h *handler) GenerateKubernetesAttributes(ctx context.Context, inst *ktmpl.Instance) (*ktmpl.Output, error) {
	out := ktmpl.NewOutput()

	if inst.DestinationUid != "" {
		if p, found := h.findPod(inst.DestinationUid); found {
			h.fillDestinationAttrs(p, inst.DestinationPort, out, h.params)
		}
	} else if inst.DestinationIp != nil && !inst.DestinationIp.IsUnspecified() {
		if p, found := h.findPod(inst.DestinationIp.String()); found {
			h.fillDestinationAttrs(p, inst.DestinationPort, out, h.params)
		}
	}

	if inst.SourceUid != "" {
		if p, found := h.findPod(inst.SourceUid); found {
			h.fillSourceAttrs(p, out, h.params)
		}
	} else if inst.SourceIp != nil && !inst.SourceIp.IsUnspecified() {
		if p, found := h.findPod(inst.SourceIp.String()); found {
			h.fillSourceAttrs(p, out, h.params)
		}
	}

	return out, nil
}

func (h *handler) Close() error {
	return nil
}

func (h *handler) findPod(uid string) (*v1.Pod, bool) {
	podKey := keyFromUID(uid)
	pod, found := h.k8sCache.Pod(podKey)
	if !found {
		h.env.Logger().Debugf("could not find pod for (uid: %s, key: %s)", uid, podKey)
	}
	return pod, found
}

func keyFromUID(uid string) string {
	if ip := net.ParseIP(uid); ip != nil {
		return uid
	}
	fullname := strings.TrimPrefix(uid, kubePrefix)
	if strings.Contains(fullname, ".") {
		parts := strings.Split(fullname, ".")
		if len(parts) == 2 {
			return key(parts[1], parts[0])
		}
	}
	return fullname
}

func findContainer(p *v1.Pod, port int64) string {
	if port <= 0 {
		return ""
	}
	for _, c := range p.Spec.Containers {
		for _, cp := range c.Ports {
			if int64(cp.ContainerPort) == port {
				return c.Name
			}
		}
	}
	return ""
}

func (h *handler) fillDestinationAttrs(p *v1.Pod, port int64, o *ktmpl.Output, params *config.Params) {
	if len(p.Labels) > 0 {
		o.SetDestinationLabels(p.Labels)
	}
	if len(p.Name) > 0 {
		o.SetDestinationPodName(p.Name)
	}
	if len(p.Namespace) > 0 {
		o.SetDestinationNamespace(p.Namespace)
	}
	if len(p.Name) > 0 && len(p.Namespace) > 0 {
		o.SetDestinationPodUid(kubePrefix + p.Name + "." + p.Namespace)
	}
	if len(p.Spec.ServiceAccountName) > 0 {
		o.SetDestinationServiceAccountName(p.Spec.ServiceAccountName)
	}
	if len(p.Status.PodIP) > 0 {
		o.SetDestinationPodIp(net.ParseIP(p.Status.PodIP))
	}
	if len(p.Status.HostIP) > 0 {
		o.SetDestinationHostIp(net.ParseIP(p.Status.HostIP))
	}
	if wl, found := h.k8sCache.Workload(p); found {
		o.SetDestinationWorkloadUid(wl.uid)
		o.SetDestinationWorkloadName(wl.name)
		o.SetDestinationWorkloadNamespace(wl.namespace)
		if len(wl.selfLinkURL) > 0 {
			o.SetDestinationOwner(wl.selfLinkURL)
		}
	}
	if cn := findContainer(p, port); cn != "" {
		o.SetDestinationContainerName(cn)
	}
}

func (h *handler) fillSourceAttrs(p *v1.Pod, o *ktmpl.Output, params *config.Params) {
	if len(p.Labels) > 0 {
		o.SetSourceLabels(p.Labels)
	}
	if len(p.Name) > 0 {
		o.SetSourcePodName(p.Name)
	}
	if len(p.Namespace) > 0 {
		o.SetSourceNamespace(p.Namespace)
	}
	if len(p.Name) > 0 && len(p.Namespace) > 0 {
		o.SetSourcePodUid(kubePrefix + p.Name + "." + p.Namespace)
	}
	if len(p.Spec.ServiceAccountName) > 0 {
		o.SetSourceServiceAccountName(p.Spec.ServiceAccountName)
	}
	if len(p.Status.PodIP) > 0 {
		o.SetSourcePodIp(net.ParseIP(p.Status.PodIP))
	}
	if len(p.Status.HostIP) > 0 {
		o.SetSourceHostIp(net.ParseIP(p.Status.HostIP))
	}
	if wl, found := h.k8sCache.Workload(p); found {
		o.SetSourceWorkloadUid(wl.uid)
		o.SetSourceWorkloadName(wl.name)
		o.SetSourceWorkloadNamespace(wl.namespace)
		if len(wl.selfLinkURL) > 0 {
			o.SetSourceOwner(wl.selfLinkURL)
		}
	}
}

func newKubernetesClient(kubeconfigPath string, env adapter.Env) (k8s.Interface, error) {
	env.Logger().Infof("getting kubeconfig from: %#v", kubeconfigPath)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil || config == nil {
		return nil, fmt.Errorf("could not retrieve kubeconfig: %v", err)
	}
	return k8s.NewForConfig(config)
}
