package exporter

import (
	"fmt"
	"istio.io/istio/pilot/pkg/registeragent/exporter/default"
	"istio.io/istio/pilot/pkg/registeragent/exporter/hsf"
	"os"
)

func RpcInfoExporterFactory() (r RpcAcutator) {
	rpcType := os.Getenv("RPC_TYPE")
	fmt.Printf("rpcType: %s", rpcType)
	switch rpcType {
	case "HSF":
		r = hsf.NewRpcInfoExporter()
	default:
		r = _default.NewRpcInfoExporter()
	}
	return r
}
