package main

import (
	"fmt"
	log "github.com/cihub/seelog"
	"github.com/gin-gonic/gin"
	"io"
	"istio.io/istio/pilot/pkg/config/registeragent"
	"istio.io/istio/pilot/pkg/registeragent/exporter"
	"os"
	"path"
	"runtime"
	"time"
)

func initLog(logPath string) {
	configStr := `
        <seelog>         <outputs formatid="main">
                 <filter levels="debug,info,warn,error">
                     <buffered size="10000" flushperiod="1000">
                         <rollingfile type="date" filename="%s" datepattern="2006-01-02" maxrolls="30"/>
                     </buffered>
                </filter>
            </outputs>
            <formats>
                <format id="main" format="[%%LEVEL %%Time %%File:%%Line] %%Msg%%n"/>
            </formats>
        </seelog>`

	os.MkdirAll(logPath, 0777)
	config := fmt.Sprintf(configStr, path.Join(logPath, "register-agent.log"))
	logger, _ := log.LoggerFromConfigAsBytes([]byte(config))
	log.UseLogger(logger)
	f, _ := os.Create(path.Join(logPath, "gin.log"))
	gin.DefaultWriter = io.MultiWriter(f)
}

func init() {
	time.LoadLocation("China/BeiJing")
	initLog("./logs/register-agent")
}

func startExporter(conf *config.Config) {
	runtime.GOMAXPROCS(4)
	time.LoadLocation("China/BeiJing")
	router := gin.Default()
	rpcAcutatorExporter := exporter.RpcInfoExporterFactory()
	router.GET("/rpc/interfaces", rpcAcutatorExporter.GetRpcServiceInfo)
	router.Run(":10006")
}

func main() {
	config := config.NewConfig()
	log.Info(config)
	startExporter(config)
}
