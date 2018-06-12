package dummy

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

type dummy struct{}

// NewPlugin instantiates a new plugin.
func NewPlugin() plugin.Plugin {
	return dummy{}
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service.
func (dummy) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node.Type == model.Router {
		if mutable.Listener.Metadata == nil {
			mutable.Listener.Metadata = &core.Metadata{}
		}
		if mutable.Listener.Metadata.FilterMetadata == nil {
			mutable.Listener.Metadata.FilterMetadata = make(map[string]*types.Struct)
		}
		mutable.Listener.Metadata.FilterMetadata["dummy"] = &types.Struct{
			Fields: map[string]*types.Value{
				"dummy": &types.Value{Kind: &types.Value_BoolValue{BoolValue: true}},
			},
		}
	}
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
func (dummy) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

// OnOutboundCluster is called whenever a new cluster is added to the CDS output.
func (dummy) OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
	cluster *xdsapi.Cluster) {
}

// OnInboundCluster is called whenever a new cluster is added to the CDS output.
func (dummy) OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
	cluster *xdsapi.Cluster) {
}

// OnOutboundRouteConfiguration is called whenever a new set of virtual hosts (a set of virtual hosts with routes) is
func (dummy) OnOutboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}

// OnInboundRouteConfiguration is called whenever a new set of virtual hosts are added to the inbound path.
func (dummy) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}
