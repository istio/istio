package constants

import (
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
)

const (
	WasmHTTPFilterType = resource.APITypePrefix + wellknown.HTTPWasm
	TypedStructType    = resource.APITypePrefix + "udpa.type.v1.TypedStruct"

	StatsFilterName       = "istio.stats"
	StackdriverFilterName = "istio.stackdriver"
)
