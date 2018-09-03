package main

import (
	"os"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"istio.io/istio/pilot/pkg/config/registeragent"
	"istio.io/istio/pilot/pkg/registeragent/exporter"
	"istio.io/istio/pkg/log"
)

var (
	flags = struct {
		loggingOptions *log.Options
	}{
		loggingOptions: log.DefaultOptions(),
	}
)

func init() {
	time.LoadLocation("China/BeiJing")
	if err := log.Configure(flags.loggingOptions); err != nil {
		os.Exit(-1)
	}
}

func startExporter(conf *config.Config) {
	runtime.GOMAXPROCS(4)
	router := gin.Default()
	rpcAcutatorExporter := exporter.RpcInfoExporterFactory()
	router.GET("/rpc/interfaces", rpcAcutatorExporter.GetRpcServiceInfo)
	router.Run(":10006")
}

func main() {
	config := config.NewConfig()
	log.Infof("all configs %s", config)
	startExporter(config)
}
