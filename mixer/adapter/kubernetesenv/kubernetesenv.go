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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/kubernetesenv/config/config.proto -x "-n kubernetesenv -d example"
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/adapter/kubernetesenv/template/template.proto

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
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	k8s "k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // needed for auth
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/pkg/env"

	"istio.io/istio/mixer/adapter/kubernetesenv/config"
	ktmpl "istio.io/istio/mixer/adapter/kubernetesenv/template"
	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
)

const (
	// parsing
	kubePrefix = "kubernetes://"

	// k8s cache invalidation
	// TODO: determine a reasonable default
	defaultRefreshPeriod = 5 * time.Minute

	defaultClusterRegistriesNamespace = "istio-system"

	mixerClusterControllerPrefix = "mixer-cluster-"
)

var (
	conf = &config.Params{
		KubeconfigPath:             "",
		CacheRefreshDuration:       defaultRefreshPeriod,
		ClusterRegistriesNamespace: "",
	}
)

type (
	builder struct {
		adapterConfig *config.Params
		newClientFn   clientFactoryFn

		sync.Mutex
		controllers map[string]cacheController

		kubeHandler             *handler
		multiClusterWatcherInit sync.Once
	}

	handler struct {
		sync.RWMutex
		k8sCache map[string]cacheController
		env      adapter.Env
		params   *config.Params

		builder *builder
	}

	// used strictly for testing purposes
	clientFactoryFn func(kubeconfigPath string, env adapter.Env) (k8s.Interface, error)
)

// compile-time validation
var _ ktmpl.Handler = &handler{}
var _ ktmpl.HandlerBuilder = &builder{}

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	singletonBuilder := newBuilder(newKubernetesClient)
	info := metadata.GetInfo("kubernetesenv")
	info.NewBuilder = func() adapter.HandlerBuilder { return singletonBuilder }
	return info
}

func (b *builder) SetAdapterConfig(c adapter.Config) {
	b.adapterConfig = c.(*config.Params)
}

// Validate is responsible for ensuring that all the configuration state given to the builder is
// correct.
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	return
}

var kubeConfigVar = env.RegisterStringVar("KUBECONFIG", "", "Path for a kubeconfig file.")

func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	paramsProto := b.adapterConfig
	var controller cacheController
	var controllers = make(map[string]cacheController)

	path, exists := kubeConfigVar.Lookup()
	if !exists {
		path = paramsProto.KubeconfigPath
	}

	// only ever build a controller for a config once. this potential blocks
	// the Build() for multiple handlers using the same config until the first
	// one has synced. This should be OK, as the WaitForCacheSync was meant to
	// provide this basic functionality before.
	b.Lock()
	defer b.Unlock()
	_, found := b.controllers[mixerClusterControllerPrefix+path]
	if !found {
		clientset, err := b.newClientFn(path, env)
		if err != nil {
			return nil, fmt.Errorf("could not build kubernetes client: %v", err)
		}
		controller, err = runNewController(b, clientset, env)
		if err != nil {
			return nil, fmt.Errorf("could not create new cache controller: %v", err)
		}
		controllers[mixerClusterControllerPrefix+path] = controller
		b.controllers[mixerClusterControllerPrefix+path] = controller
	} else {
		for clusterID := range b.controllers {
			controllers[clusterID] = b.controllers[clusterID]
		}
	}

	kubeHandler := handler{
		env:      env,
		k8sCache: controllers,
		params:   paramsProto,
		builder:  b,
	}

	b.kubeHandler = &kubeHandler

	if !found {
		// only init a single watcher for multicluster secrets for a given Builder.
		// if init fails, try again on next Build().
		// This should prevent an ever-growing number of watchers for a Builder.
		var initErr error
		b.multiClusterWatcherInit.Do(func() {
			initErr = initMultiClusterSecretController(b, path, env)
		})
		if initErr != nil {
			b.multiClusterWatcherInit = sync.Once{}
			return nil, fmt.Errorf("could not create remote controllers: %v", initErr)
		}
	}

	return &kubeHandler, nil
}

func runNewController(b *builder, clientset k8s.Interface, env adapter.Env) (cacheController, error) {
	paramsProto := b.adapterConfig
	stopChan := make(chan struct{})
	refresh := paramsProto.CacheRefreshDuration

	controller := newCacheController(clientset, refresh, env, stopChan)
	env.ScheduleDaemon(func() { controller.Run(stopChan) })

	// ensure that any request is only handled after
	// a sync has occurred
	env.Logger().Infof("Waiting for kubernetes cache sync...")
	if success := cache.WaitForCacheSync(stopChan, controller.HasSynced); !success {
		stopChan <- struct{}{}
		return nil, errors.New("cache sync failure")
	}
	env.Logger().Infof("Cache sync successful.")

	return controller, nil
}

func newBuilder(clientFactory clientFactoryFn) *builder {
	return &builder{
		newClientFn:             clientFactory,
		controllers:             make(map[string]cacheController),
		adapterConfig:           conf,
		multiClusterWatcherInit: sync.Once{},
	}
}

func (h *handler) GenerateKubernetesAttributes(ctx context.Context, inst *ktmpl.Instance) (*ktmpl.Output, error) {
	out := ktmpl.NewOutput()

	if inst.DestinationUid != "" {
		if c, p, found := h.findPod(inst.DestinationUid); found {
			h.fillDestinationAttrs(c, p, inst.DestinationPort, out)
		}
	} else if inst.DestinationIp != nil && !inst.DestinationIp.IsUnspecified() {
		if c, p, found := h.findPod(inst.DestinationIp.String()); found {
			h.fillDestinationAttrs(c, p, inst.DestinationPort, out)
		}
	}

	if inst.SourceUid != "" {
		if c, p, found := h.findPod(inst.SourceUid); found {
			h.fillSourceAttrs(c, p, out)
		}
	} else if inst.SourceIp != nil && !inst.SourceIp.IsUnspecified() {
		if c, p, found := h.findPod(inst.SourceIp.String()); found {
			h.fillSourceAttrs(c, p, out)
		}
	}

	return out, nil
}

func (h *handler) Close() error {
	for clusterID := range h.k8sCache {
		if !strings.HasPrefix(clusterID, mixerClusterControllerPrefix) {
			_ = h.builder.deleteCacheController(clusterID)
		}
	}
	h.builder.Lock()
	h.builder.kubeHandler = nil
	h.builder.Unlock()

	return nil
}

func (h *handler) findPod(uid string) (cacheController, *v1.Pod, bool) {
	podKey := keyFromUID(uid)
	var found bool
	var pod *v1.Pod
	var c cacheController

	h.RLock()
	defer h.RUnlock()
	for _, controller := range h.k8sCache {
		pod, found = controller.Pod(podKey)
		if found {
			c = controller
			break
		}
	}

	if !found {
		h.env.Logger().Debugf("could not find pod for (uid: %s, key: %s)", uid, podKey)
	}
	return c, pod, found
}

//The name of workload may contain '.'
func keyFromUID(uid string) string {
	if ip := net.ParseIP(uid); ip != nil {
		return uid
	}
	fullname := strings.TrimPrefix(uid, kubePrefix)
	dotPos := strings.LastIndex(fullname, ".")
	if dotPos != -1 {
		return key(fullname[dotPos+1:], fullname[:dotPos])
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

func (h *handler) fillDestinationAttrs(c cacheController, p *v1.Pod, port int64, o *ktmpl.Output) {
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

	wl := c.Workload(p)
	o.SetDestinationWorkloadUid(wl.uid)
	o.SetDestinationWorkloadName(wl.name)
	o.SetDestinationWorkloadNamespace(wl.namespace)
	if len(wl.selfLinkURL) > 0 {
		o.SetDestinationOwner(wl.selfLinkURL)
	}

	if cn := findContainer(p, port); cn != "" {
		o.SetDestinationContainerName(cn)
	}
}

func (h *handler) fillSourceAttrs(c cacheController, p *v1.Pod, o *ktmpl.Output) {
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

	wl := c.Workload(p)
	o.SetSourceWorkloadUid(wl.uid)
	o.SetSourceWorkloadName(wl.name)
	o.SetSourceWorkloadNamespace(wl.namespace)
	if len(wl.selfLinkURL) > 0 {
		o.SetSourceOwner(wl.selfLinkURL)
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

func (b *builder) createCacheController(clients kube.Client, clusterID string) error {
	controller, err := runNewController(b, clients.Kube(), b.kubeHandler.env)
	if err == nil {
		b.Lock()
		b.controllers[clusterID] = controller
		b.Unlock()

		b.kubeHandler.Lock()
		b.kubeHandler.k8sCache[clusterID] = controller
		b.kubeHandler.Unlock()

		b.kubeHandler.env.Logger().Infof("created remote controller %s", clusterID)
		return nil
	}

	return b.kubeHandler.env.Logger().Errorf("error on creating remote controller %s err = %v", clusterID, err)
}

func (b *builder) updateCacheController(clients kube.Client, clusterID string) error {
	if err := b.deleteCacheController(clusterID); err != nil {
		return err
	}
	return b.createCacheController(clients, clusterID)
}

func (b *builder) deleteCacheController(clusterID string) error {
	b.Lock()
	delete(b.controllers, clusterID)
	b.Unlock()

	b.kubeHandler.Lock()
	defer b.kubeHandler.Unlock()
	b.kubeHandler.k8sCache[clusterID].StopControlChannel()
	delete(b.kubeHandler.k8sCache, clusterID)

	b.kubeHandler.env.Logger().Infof("deleted remote controller %s", clusterID)

	return nil
}

var clusterNsVar = env.RegisterStringVar("POD_NAMESPACE", defaultClusterRegistriesNamespace, "Namespace for the Mixer pod (Downward API).")

func initMultiClusterSecretController(b *builder, kubeconfig string, env adapter.Env) (err error) {
	var clusterNs string

	paramsProto := b.adapterConfig
	if clusterNs = paramsProto.ClusterRegistriesNamespace; clusterNs == "" {
		clusterNs = clusterNsVar.Get()
	}

	kubeClient, err := b.newClientFn(kubeconfig, env)
	if err != nil {
		return fmt.Errorf("could not create K8s client: %v", err)
	}

	_ = secretcontroller.StartSecretController(kubeClient, b.createCacheController,
		b.updateCacheController, b.deleteCacheController, clusterNs)

	return nil
}
