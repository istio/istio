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
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

type MeshConfig = meshwatcher.MeshConfigResource

type Outputs struct {
	MergedVirtualServices krt.Collection[MergedVirtualService]
}

type VSControllerOptions struct {
	KrtDebugger *krt.DebugHandler
	XDSUpdater  XDSUpdater
}

type VirtualServiceController struct {
	handlers []krt.HandlerRegistration

	outputs Outputs

	xdsUpdater XDSUpdater

	stop chan struct{}
}

func NewVirtualServiceController(
	store ConfigStoreController,
	options VSControllerOptions,
	meshConfig meshwatcher.WatcherCollection,
) *VirtualServiceController {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "virtualservice", options.KrtDebugger)

	c := &VirtualServiceController{
		xdsUpdater: options.XDSUpdater,
		stop:       stop,
	}

	VirtualServices := store.KrtCollection(gvk.VirtualService)
	if VirtualServices == nil {
		panic("VirtualServices is nil")
	}

	DefaultExportTo := defaultExportTo(
		meshConfig.AsCollection(),
		opts,
	)

	DelegateVirtualServices := delegateVirtualServices(
		VirtualServices,
		DefaultExportTo.AsCollection(),
		opts,
	)

	MergedVirtualServices := mergeVirtualServices(
		VirtualServices,
		DelegateVirtualServices,
		DefaultExportTo.AsCollection(),
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

// xdsPush fires a config push for VirtualService events, applying the same suppression
// logic as NeedsPush: skip updates where the spec is unchanged AND no istio.io
// label/annotation changed. Non-istio.io metadata changes (Helm, Argo CD,
// kubectl annotations) are not sufficient to trigger a push.
func (c *VirtualServiceController) xdsPush(events []krt.Event[MergedVirtualService]) {
	if c.xdsUpdater == nil {
		return
	}

	cu := sets.New[ConfigKey]()
	for _, e := range events {
		if e.Old != nil && e.New != nil && !NeedsPush(*e.Old.Config, *e.New.Config) {
			continue
		}
		for _, vs := range e.Items() {
			cu.Insert(ConfigKey{
				Kind:      kind.VirtualService,
				Name:      vs.Name,
				Namespace: vs.Namespace,
			})
		}
	}

	if len(cu) == 0 {
		return
	}

	c.xdsUpdater.ConfigUpdate(&PushRequest{
		ConfigsUpdated: cu,
		Reason:         NewReasonStats(ConfigUpdate),
	})
}

func (c *VirtualServiceController) MergedVirtualServices() []MergedVirtualService {
	return sortMergedVirtualServicesByCreationTime(c.outputs.MergedVirtualServices.List())
}

func (c *VirtualServiceController) Collection() krt.Collection[MergedVirtualService] {
	return c.outputs.MergedVirtualServices
}

func (c *VirtualServiceController) Run(stop <-chan struct{}) {
	<-stop
	close(c.stop)
}

func (c *VirtualServiceController) HasSynced() bool {
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

func delegateVirtualServices(
	virtualServices krt.Collection[config.Config],
	defaultExportTo krt.Collection[DefaultExportToSet],
	opts krt.OptionsBuilder,
) krt.Collection[DelegateVirtualService] {
	return krt.NewCollection(virtualServices, func(ctx krt.HandlerContext, cfg config.Config) *DelegateVirtualService {
		spec := cfg.Spec.(*networking.VirtualService)
		// this is a Root VS, we won't add these to the collection directly
		if len(spec.Hosts) > 0 {
			return nil
		}

		exportToSet := convertExportToSet(spec.ExportTo, cfg.Namespace)
		if len(exportToSet) == 0 {
			// No exportTo in virtualService. Use the global default
			defaultExportTo := krt.FetchOne(ctx, defaultExportTo).Set
			exportToSet = copyDefaultExportToSet(defaultExportTo, cfg.Namespace)
		}

		return &DelegateVirtualService{
			Spec:      ResolveVirtualServiceShortnames(cfg).Spec.(*networking.VirtualService),
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
			ExportTo:  exportToSet,
		}
	}, opts.WithName("DelegateVirtualServices")...)
}

type DefaultExportToSet struct {
	sets.Set[visibility.Instance]
}

func (e DefaultExportToSet) ResourceName() string {
	return "default_export_to"
}

func (e DefaultExportToSet) Equals(other DefaultExportToSet) bool {
	return e.Set.Equals(other.Set)
}

func defaultExportTo(
	meshConfig krt.Collection[MeshConfig],
	opts krt.OptionsBuilder,
) krt.Singleton[DefaultExportToSet] {
	return krt.NewSingleton(func(ctx krt.HandlerContext) *DefaultExportToSet {
		meshCfg := krt.FetchOne(ctx, meshConfig)

		exports := sets.New[visibility.Instance]()
		if meshCfg.DefaultVirtualServiceExportTo != nil {
			for _, e := range meshCfg.DefaultVirtualServiceExportTo {
				exports.Insert(visibility.Instance(e))
			}
		} else {
			exports.Insert(visibility.Public)
		}

		return &DefaultExportToSet{Set: exports}
	}, opts.WithName("DefaultExportTo")...)
}

type MergedVirtualService struct {
	*config.Config
	ExportTo sets.Set[visibility.Instance]
}

func (m MergedVirtualService) ResourceName() string {
	return m.Namespace + "/" + m.Name
}

func (m MergedVirtualService) Equals(other MergedVirtualService) bool {
	return m.Config.Equals(other.Config)
}

func mergeVirtualServices(
	virtualServices krt.Collection[config.Config],
	delegateVirtualServices krt.Collection[DelegateVirtualService],
	defaultExportTo krt.Collection[DefaultExportToSet],
	opts krt.OptionsBuilder,
) krt.Collection[MergedVirtualService] {
	return krt.NewCollection(virtualServices, func(ctx krt.HandlerContext, cfg config.Config) *MergedVirtualService {
		// this is a Delegate VS, we won't add these to the collection directly
		spec := cfg.Spec.(*networking.VirtualService)
		if len(spec.Hosts) == 0 {
			return nil
		}

		// if this is a Gateway VS, we don't need to perform any merging or short name resolving
		if UseGatewaySemantics(cfg) {
			exportToSet := convertExportToSet(spec.ExportTo, cfg.Namespace)
			if len(exportToSet) == 0 {
				defaultExportTo := krt.FetchOne(ctx, defaultExportTo).Set
				exportToSet = copyDefaultExportToSet(defaultExportTo, cfg.Namespace)
			}
			return &MergedVirtualService{
				Config:   &cfg,
				ExportTo: exportToSet,
			}
		}

		root := ResolveVirtualServiceShortnames(cfg)
		spec = root.Spec.(*networking.VirtualService)

		exportToSet := convertExportToSet(spec.ExportTo, cfg.Namespace)
		if len(exportToSet) == 0 {
			defaultExportTo := krt.FetchOne(ctx, defaultExportTo).Set
			exportToSet = copyDefaultExportToSet(defaultExportTo, cfg.Namespace)
		}

		// if this is an Ingress VS, we don't need to perform any merging
		if UseIngressSemantics(cfg) {
			return &MergedVirtualService{
				Config:   &root,
				ExportTo: exportToSet,
			}
		}

		// if this VS does not reference any delegate, we don't need to perform any merging
		if !isRootVs(spec) {
			return &MergedVirtualService{
				Config:   &root,
				ExportTo: exportToSet,
			}
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

		// this modification is OK because ResolveVirtualServiceShortnames already deep copied the spec
		spec.Http = mergedRoutes
		if log.DebugEnabled() {
			vsString, _ := protomarshal.ToJSONWithIndent(spec, "   ")
			log.Debugf("merged virtualService: %s", vsString)
		}

		return &MergedVirtualService{
			Config:   &root,
			ExportTo: exportToSet,
		}
	}, opts.WithName("MergedVirtualServices")...)
}

func convertExportToSet(exportTo []string, namespace string) sets.Set[visibility.Instance] {
	if len(exportTo) == 0 {
		return nil
	}

	exportToSet := sets.NewWithLength[visibility.Instance](len(exportTo))
	for _, e := range exportTo {
		v := visibility.Instance(e)
		if v == visibility.Private {
			v = visibility.Instance(namespace)
		}
		exportToSet.Insert(v)
	}
	return exportToSet
}

func copyDefaultExportToSet(defaultExportTo sets.Set[visibility.Instance], namespace string) sets.Set[visibility.Instance] {
	exportToSet := sets.NewWithLength[visibility.Instance](defaultExportTo.Len())
	for v := range defaultExportTo {
		if v == visibility.Private {
			exportToSet.Insert(visibility.Instance(namespace))
		} else {
			exportToSet.Insert(v)
		}
	}
	return exportToSet
}

// sortMergedVirtualServicesByCreationTime sorts the list of merged virtual services in ascending order by their creation time (if available)
func sortMergedVirtualServicesByCreationTime(configs []MergedVirtualService) []MergedVirtualService {
	slices.SortFunc(configs, func(a, b MergedVirtualService) int {
		return configCompareByCreationTime(*a.Config, *b.Config)
	})
	return configs
}
