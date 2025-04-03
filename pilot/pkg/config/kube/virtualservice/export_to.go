package virtualservice

import (
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/sets"
)

type ExportTo struct {
	Set sets.Set[visibility.Instance]
}

func (e ExportTo) ResourceName() string {
	return "export_to"
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
