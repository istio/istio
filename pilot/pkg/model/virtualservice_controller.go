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

package model

import (
	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

type MeshConfig = meshwatcher.MeshConfigResource

type Outputs struct {
	MergedVirtualServices krt.Collection[config.Config]
}

type VSControllerOptions struct {
	KrtDebugger *krt.DebugHandler
	XDSUpdater  XDSUpdater
}

type VirtualServiceController struct {
	store ConfigStoreController

	handlers []krt.HandlerRegistration

	outputs Outputs

	xdsUpdater XDSUpdater

	stop chan struct{}
}

var _ ConfigStoreController = &VirtualServiceController{}

func NewVirtualServiceController(
	store ConfigStoreController,
	options VSControllerOptions,
	meshConfig meshwatcher.WatcherCollection,
) *VirtualServiceController {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "virtualservice", options.KrtDebugger)

	c := &VirtualServiceController{
		store:      store,
		xdsUpdater: options.XDSUpdater,
		stop:       stop,
	}

	VirtualServices := store.KrtCollection(gvk.VirtualService)
	if VirtualServices == nil {
		panic("VirtualServices is nil")
	}

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

	c.handlers = append(
		c.handlers,
		MergedVirtualServices.RegisterBatch(c.xdsPush, false),
	)
	c.outputs = Outputs{
		MergedVirtualServices: MergedVirtualServices,
	}

	return c
}

func (c *VirtualServiceController) xdsPush(events []krt.Event[config.Config]) {
	if c.xdsUpdater == nil {
		return
	}

	cu := sets.New[ConfigKey]()
	for _, e := range events {
		for _, vs := range e.Items() {
			c := ConfigKey{
				Kind:      kind.VirtualService,
				Name:      vs.Name,
				Namespace: vs.Namespace,
			}
			cu.Insert(c)
		}
	}

	if len(cu) == 0 {
		return
	}

	c.xdsUpdater.ConfigUpdate(&PushRequest{
		Full:           true,
		ConfigsUpdated: cu,
		Reason:         NewReasonStats(ConfigUpdate),
	})
}

func (c *VirtualServiceController) Schemas() collection.Schemas {
	return c.store.Schemas()
}

func (c *VirtualServiceController) KrtCollection(typ config.GroupVersionKind) krt.Collection[config.Config] {
	if typ != gvk.VirtualService {
		return c.store.KrtCollection(typ)
	}

	return c.outputs.MergedVirtualServices
}

func (c *VirtualServiceController) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	if typ != gvk.VirtualService {
		return c.store.Get(typ, name, namespace)
	}

	return nil
}

func (c *VirtualServiceController) List(typ config.GroupVersionKind, namespace string) []config.Config {
	if typ != gvk.VirtualService {
		return c.store.List(typ, namespace)
	}

	return sortConfigByCreationTime(c.outputs.MergedVirtualServices.List())
}

func (c *VirtualServiceController) Create(config config.Config) (revision string, err error) {
	return c.store.Create(config)
}

func (c *VirtualServiceController) Update(config config.Config) (newRevision string, err error) {
	return c.store.Update(config)
}

func (c *VirtualServiceController) UpdateStatus(config config.Config) (newRevision string, err error) {
	return c.store.UpdateStatus(config)
}

func (c *VirtualServiceController) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return c.store.Patch(orig, patchFn)
}

func (c *VirtualServiceController) Delete(typ config.GroupVersionKind, name, namespace string, _ *string) error {
	return c.store.Delete(typ, name, namespace, nil)
}

func (c *VirtualServiceController) RegisterEventHandler(typ config.GroupVersionKind, handler EventHandler) {
	if typ != gvk.VirtualService {
		c.store.RegisterEventHandler(typ, handler)
	}
}

func (c *VirtualServiceController) Run(stop <-chan struct{}) {
	go c.store.Run(stop)
	<-stop
	close(c.stop)
}

func (c *VirtualServiceController) HasSynced() bool {
	if !c.store.HasSynced() {
		return false
	}

	if !c.outputs.MergedVirtualServices.HasSynced() {
		return false
	}

	for _, h := range c.handlers {
		if !h.HasSynced() {
			return false
		}
	}

	return true
}

// DelegateVirtualService is a wrapper around a VirtualService that represents a delegate
// VirtualService. It contains the VirtualService's Spec, Name, Namespace, and processed ExportTo.
type DelegateVirtualService struct {
	Spec      *networking.VirtualService
	Name      string
	Namespace string
	ExportTo  sets.Set[visibility.Instance]
}

func (dvs DelegateVirtualService) ResourceName() string {
	return types.NamespacedName{Namespace: dvs.Namespace, Name: dvs.Name}.String()
}

func (dvs DelegateVirtualService) Equals(other DelegateVirtualService) bool {
	return dvs.ExportTo.Equals(other.ExportTo) && protoconv.Equals(dvs.Spec, other.Spec)
}

func DelegateVirtualServices(
	virtualServices krt.Collection[config.Config],
	defaultExportTo krt.Collection[ExportTo],
	opts krt.OptionsBuilder,
) krt.Collection[DelegateVirtualService] {
	return krt.NewCollection(virtualServices, func(ctx krt.HandlerContext, cfg config.Config) *DelegateVirtualService {
		spec := cfg.Spec.(*networking.VirtualService)
		// this is a Root VS, we won't add these to the collection directly
		if len(spec.Hosts) > 0 {
			return nil
		}

		var exportToSet sets.Set[visibility.Instance]
		if len(spec.ExportTo) == 0 {
			// No exportTo in virtualService. Use the global default
			defaultExportTo := krt.FetchOne(ctx, defaultExportTo).Set
			exportToSet = sets.NewWithLength[visibility.Instance](defaultExportTo.Len())
			for v := range defaultExportTo {
				if v == visibility.Private {
					exportToSet.Insert(visibility.Instance(cfg.Namespace))
				} else {
					exportToSet.Insert(v)
				}
			}
		} else {
			exportToSet = sets.NewWithLength[visibility.Instance](len(spec.ExportTo))
			for _, e := range spec.ExportTo {
				if e == string(visibility.Private) {
					exportToSet.Insert(visibility.Instance(cfg.Namespace))
				} else {
					exportToSet.Insert(visibility.Instance(e))
				}
			}
		}

		return &DelegateVirtualService{
			Spec:      ResolveVirtualServiceShortnames(cfg).Spec.(*networking.VirtualService),
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
			ExportTo:  exportToSet,
		}
	}, opts.WithName("DelegateVirtualServices")...)
}

type ExportTo struct {
	Set sets.Set[visibility.Instance]
}

func (e ExportTo) ResourceName() string {
	return "export_to"
}

func (e ExportTo) Equals(other ExportTo) bool {
	return e.Set.Equals(other.Set)
}

func DefaultExportTo(
	meshConfig krt.Collection[MeshConfig],
	opts krt.OptionsBuilder,
) krt.Singleton[ExportTo] {
	return krt.NewSingleton(func(ctx krt.HandlerContext) *ExportTo {
		meshCfg := krt.FetchOne(ctx, meshConfig)

		exports := sets.New[visibility.Instance]()
		if meshCfg.DefaultVirtualServiceExportTo != nil {
			for _, e := range meshCfg.DefaultVirtualServiceExportTo {
				exports.Insert(visibility.Instance(e))
			}
		} else {
			exports.Insert(visibility.Public)
		}

		return &ExportTo{Set: exports}
	}, opts.WithName("DefaultExportTo")...)
}

func MergeVirtualServices(
	virtualServices krt.Collection[config.Config],
	delegateVirtualServices krt.Collection[DelegateVirtualService],
	opts krt.OptionsBuilder,
) krt.Collection[config.Config] {
	return krt.NewCollection(virtualServices, func(ctx krt.HandlerContext, cfg config.Config) *config.Config {
		// this is a Delegate VS, we won't add these to the collection directly
		if len(cfg.Spec.(*networking.VirtualService).Hosts) == 0 {
			return nil
		}

		root := ResolveVirtualServiceShortnames(cfg)
		spec := root.Spec.(*networking.VirtualService)

		// if this VS does not reference any delegate, we don't need to perform any merging
		if !isRootVs(spec) {
			return &root
		}

		mergedRoutes := []*networking.HTTPRoute{}
		for _, http := range spec.Http {
			if delegate := http.Delegate; delegate != nil {
				delegateNamespace := delegate.Namespace
				if delegateNamespace == "" {
					delegateNamespace = root.Namespace
				}

				key := types.NamespacedName{Namespace: delegateNamespace, Name: delegate.Name}
				delegateVs := krt.FetchOne(ctx, delegateVirtualServices, krt.FilterObjectName(key))
				if delegateVs == nil {
					log.Warnf("delegate virtual service %s/%s of %s/%s not found",
						delegateNamespace, delegate.Name, root.Namespace, root.Name)
					// delegate not found, ignore only the current HTTP route
					continue
				}

				// make sure that the delegate is visible to root virtual service's namespace
				if !delegateVs.ExportTo.Contains(visibility.Public) && !delegateVs.ExportTo.Contains(visibility.Instance(root.Namespace)) {
					log.Warnf("delegate virtual service %s/%s of %s/%s is not exported to %s",
						delegateNamespace, delegate.Name, root.Namespace, root.Name, root.Namespace)
					continue
				}

				// DeepCopy to prevent mutate the original delegate, it can conflict
				// when multiple routes delegate to one single VS.
				copiedDelegate := delegateVs.Spec.DeepCopy()
				merged := MergeHTTPRoutes(http, copiedDelegate.Http)
				mergedRoutes = append(mergedRoutes, merged...)
			} else {
				mergedRoutes = append(mergedRoutes, http)
			}
		}

		spec.Http = mergedRoutes
		if log.DebugEnabled() {
			vsString, _ := protomarshal.ToJSONWithIndent(spec, "   ")
			log.Debugf("merged virtualService: %s", vsString)
		}

		return &root
	}, opts.WithName("MergedVirtualServices")...)
}
