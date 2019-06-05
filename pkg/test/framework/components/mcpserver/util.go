package mcpserver

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	"istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/sink"
)

var typeToProto = map[string]resource.Info{
	"type.googleapis.com/istio.networking.v1alpha3.Gateway": metadata.IstioNetworkingV1alpha3Gateways,
}

func transformToSinkObject(resources []*v1alpha1.Resource) ([]*sink.Object, error) {
	sinkObjects := make([]*sink.Object, 0)
	for _, pso := range resources {
		typeURL := pso.GetBody().TypeUrl
		t, known := typeToProto[typeURL]
		if !known {
			return []*sink.Object{}, fmt.Errorf("cannot find protobufType for TypeURL: %s", typeURL)
		}
		p := t.NewProtoInstance()
		err := proto.Unmarshal(pso.GetBody().Value, p)
		if err != nil {
			return []*sink.Object{}, err
		}
		so := &sink.Object{
			TypeURL:  pso.GetBody().TypeUrl,
			Metadata: pso.GetMetadata(),
			Body:     p,
		}
		sinkObjects = append(sinkObjects, so)
	}
	return sinkObjects, nil
}
