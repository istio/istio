package main

import (
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/testing/monitoring"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/fakes/mcpserver"
)

func main() {
	fakeMcpSinkPort := flag.Int("port", mcpserver.FakeServerPort, "port on which fake MCP server (in sink mode) listens")
	sinkConfigCollection := flag.String("sink-config", "", "sink mode configuration for MCP server")
	flag.Parse()

	sinkConfigs := strings.Split(*sinkConfigCollection, ",")
	so := sink.Options{
		ID:                "mcpserver.sink",
		CollectionOptions: sink.CollectionOptionsFromSlice(sinkConfigs),
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	sinkServer := mcpserver.NewMcpSinkServer(so)
	if err := sinkServer.Start(*fakeMcpSinkPort); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	sinkServer.Stop()
}
