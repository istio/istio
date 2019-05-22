package mcpserver

import (
	"github.com/gogo/protobuf/proto"
	"istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/sink"
)

func transformToSinkObject(protoSinkObjects []*v1alpha1.Resource) []*sink.Object {
	sinkObjects := make([]*sink.Object, 0)
	for _, pso := range protoSinkObjects {
		var objectBody proto.Message
		err := proto.Unmarshal(pso.GetBody().Value, objectBody)
		if err != nil {
			continue
		}
		so := &sink.Object{
			TypeURL:  pso.GetBody().TypeUrl,
			Metadata: pso.GetMetadata(),
			Body:     objectBody,
		}
		sinkObjects = append(sinkObjects, so)
	}
	return sinkObjects
}
