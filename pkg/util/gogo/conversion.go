package gogo

import (
	"bytes"

	structpb "github.com/golang/protobuf/ptypes/struct"

	gogojsonpb "github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/istio/pkg/proto"
)

func StructToProtoStruct(gogo *types.Struct) *structpb.Struct {
	if gogo == nil {
		return nil
	}
	buf := &bytes.Buffer{}
	if err := (&gogojsonpb.Marshaler{OrigName: true}).Marshal(buf, gogo); err != nil {
		return nil
	}

	pbs := &structpb.Struct{}
	if err := jsonpb.Unmarshal(buf, pbs); err != nil {
		return nil
	}
	return pbs
}

func AnyToProtoAny(gogo *types.Any) *any.Any {
	if gogo == nil {
		return nil
	}
	x := any.Any(*gogo)
	return &x
}

func BoolToProtoBool(gogo *types.BoolValue) *wrappers.BoolValue {
	if gogo == nil {
		return nil
	}
	if gogo.Value {
		return proto.BoolTrue
	}
	return proto.BoolFalse
}

func DurationToProtoDuration(gogo *types.Duration) *duration.Duration {
	if gogo == nil {
		return nil
	}
	x := duration.Duration(*gogo)
	return &x
}
