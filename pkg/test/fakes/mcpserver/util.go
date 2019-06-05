package mcpserver

import (
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	v1alpha1 "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/sink"
)

func convertToProtoSinkObjects(sos []*sink.Object) ([]*v1alpha1.Resource, error) {
	protoSinkObjects := make([]*v1alpha1.Resource, 0)
	for _, so := range sos {
		val, err := proto.Marshal(so.Body)
		if err != nil {
			return []*v1alpha1.Resource{}, err
		}
		pso := &v1alpha1.Resource{
			Metadata: so.Metadata,
			Body: &types.Any{
				TypeUrl: so.TypeURL,
				Value:   val,
			},
		}
		protoSinkObjects = append(protoSinkObjects, pso)
	}
	return protoSinkObjects, nil
}
