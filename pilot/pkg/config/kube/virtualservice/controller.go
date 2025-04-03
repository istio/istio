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

package virtualservice

import (
	"cmp"
	"fmt"
	"sort"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/util/sets"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
)

var errUnsupportedOp = fmt.Errorf("unsupported operation: the virtual service config store is a read-only view")

type MeshConfig = meshwatcher.MeshConfigResource

type Outputs struct {
	MergedVirtualServices   krt.Collection[config.Config]
	StandardVirtualServices krt.Collection[config.Config]
}

type Controller struct {
	// client for accessing Kubernetes
	client kube.Client
	// revision the controller is running under
	revision string

	handlers []krt.HandlerRegistration

	outputs Outputs

	xdsUpdater model.XDSUpdater

	stop chan struct{}
}

var _ model.ConfigStoreController = &Controller{}

func NewController(
	kc kube.Client,
	options controller.Options,
	meshConfig meshwatcher.WatcherCollection,
	xdsUpdater model.XDSUpdater,
) *Controller {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "virtualservice", options.KrtDebugger)

	c := &Controller{
		client:     kc,
		xdsUpdater: options.XDSUpdater,
		stop:       stop,
	}

	filter := kclient.Filter{
		ObjectFilter: kubetypes.ComposeFilters(kc.ObjectFilter(), c.inRevision),
	}

	VirtualServices := krt.WrapClient(
		kclient.NewDelayedInformer[*networkingclient.VirtualService](kc,
			gvr.VirtualService, kubetypes.StandardInformer, filter),
		opts.WithName("informer/VirtualServices")...,
	)

	DefaultExportTo := DefaultExportTo(
		meshConfig.AsCollection(),
		opts,
	)

	DelegateVirtualServices := DelegateVirtualServices(
		VirtualServices,
		DefaultExportTo.AsCollection(),
		opts,
	)

	MergedVirtualServices := MergeVirtualServices(
		VirtualServices,
		DelegateVirtualServices,
		opts,
	)

	StandardVirtualServices := krt.NewCollection(VirtualServices, func(ctx krt.HandlerContext, vs *networkingclient.VirtualService) *config.Config {
		// this is a Delegate VS
		if len(vs.Spec.Hosts) == 0 {
			return nil
		}

		// this is a Root VS
		if isRootVs(&vs.Spec) {
			return nil
		}

		config := model.ResolveVirtualServiceShortnames(vsToConfig(vs))
		return &config
	})

	c.handlers = append(
		c.handlers,
		MergedVirtualServices.RegisterBatch(c.xdsPush, false),
		StandardVirtualServices.RegisterBatch(c.xdsPush, false),
	)
	c.outputs = Outputs{
		MergedVirtualServices:   MergedVirtualServices,
		StandardVirtualServices: StandardVirtualServices,
	}

	return c
}

func (c *Controller) xdsPush(events []krt.Event[config.Config]) {
	if c.xdsUpdater == nil {
		return
	}

	cu := sets.New[model.ConfigKey]()
	for _, e := range events {
		vs := e.Latest()
		c := model.ConfigKey{
			Kind:      kind.VirtualService,
			Name:      vs.Name,
			Namespace: vs.Namespace,
		}
		cu.Insert(c)
	}

	if len(cu) == 0 {
		return
	}

	c.xdsUpdater.ConfigUpdate(&model.PushRequest{
		Full:           true,
		ConfigsUpdated: cu,
		Reason:         model.NewReasonStats(model.ConfigUpdate),
	})
}

func (c *Controller) inRevision(obj any) bool {
	object := controllers.ExtractObject(obj)
	if object == nil {
		return false
	}
	return config.LabelsInRevision(object.GetLabels(), c.revision)
}

func (c *Controller) Schemas() collection.Schemas {
	return collection.SchemasFor(
		collections.VirtualService,
	)
}

func (c *Controller) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	return nil
}

func (c *Controller) List(typ config.GroupVersionKind, namespace string) []config.Config {
	if typ != gvk.VirtualService {
		return nil
	}

	out := c.outputs.StandardVirtualServices.List()
	out = append(out, c.outputs.MergedVirtualServices.List()...)

	sortConfigByCreationTime(out)

	return out
}

func (c *Controller) Create(config config.Config) (revision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) Update(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) UpdateStatus(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return "", errUnsupportedOp
}

func (c *Controller) Delete(typ config.GroupVersionKind, name, namespace string, _ *string) error {
	return errUnsupportedOp
}

func (c *Controller) RegisterEventHandler(typ config.GroupVersionKind, handler model.EventHandler) {
}

func (c *Controller) Run(stop <-chan struct{}) {
	<-stop
	close(c.stop)
}

func (c *Controller) HasSynced() bool {
	if !c.outputs.MergedVirtualServices.HasSynced() {
		return false
	}

	if !c.outputs.StandardVirtualServices.HasSynced() {
		return false
	}

	for _, h := range c.handlers {
		if !h.HasSynced() {
			return false
		}
	}

	return true
}

// sortConfigByCreationTime sorts the list of config objects in ascending order by their creation time (if available)
func sortConfigByCreationTime(configs []config.Config) []config.Config {
	sort.Slice(configs, func(i, j int) bool {
		if r := configs[i].CreationTimestamp.Compare(configs[j].CreationTimestamp); r != 0 {
			return r == -1 // -1 means i is less than j, so return true
		}
		// If creation time is the same, then behavior is nondeterministic. In this case, we can
		// pick an arbitrary but consistent ordering based on name and namespace, which is unique.
		// CreationTimestamp is stored in seconds, so this is not uncommon.
		if r := cmp.Compare(configs[i].Name, configs[j].Name); r != 0 {
			return r == -1
		}
		return cmp.Compare(configs[i].Namespace, configs[j].Namespace) == -1
	})
	return configs
}
