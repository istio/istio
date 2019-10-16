package istiod

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gogo/protobuf/types"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/filewatcher"

	meshv1 "istio.io/api/mesh/v1alpha1"

	agent "istio.io/istio/pkg/bootstrap"
)

var (
	fileWatcher filewatcher.FileWatcher
)

func (s *Server) InitCommon(args *PilotArgs) {

	_, addr, err := startMonitor(args.DiscoveryOptions.MonitoringAddr, s.mux)
	if err != nil {
		return
	}
	s.MonitorListeningAddr = addr

	//go func() {
	//	<-s.stop
	//	err := monitor.Close()
	//	log.Debugf("Monitoring server terminated: %v", err)
	//}()

}

// Start all components of istio, using local config files or defaults.
//
// A minimal set of Istio Env variables are also used.
// This is expected to run in a Docker or K8S environment, with a volume with user configs mounted.
//
// Defaults:
// - http port 15007
// - grpc on 15010
//- config from $ISTIO_CONFIG or ./conf
func InitConfig(confDir string) (*Server, error) {
	baseDir := "." // TODO: env ISTIO_HOME or HOME ?

	// TODO: 15006 can't be configured currently
	// TODO: 15090 (prometheus) can't be configured. It's in the bootstrap file, so easy to replace

	meshCfgFile :=  baseDir + confDir + "/mesh"

	// Create a test pilot discovery service configured to watch the tempDir.
	args := &PilotArgs {
		DomainSuffix: "cluster.local",

		Mesh: MeshArgs{
			ConfigFile: meshCfgFile,
			RdsRefreshDelay: types.DurationProto(10 * time.Millisecond),
		},
		Config: ConfigArgs{},

		// MCP is messing up with the grpc settings...
		MCPMaxMessageSize:        1024 * 1024 * 64,
		MCPInitialWindowSize:     1024 * 1024 * 64,
		MCPInitialConnWindowSize: 1024 * 1024 * 64,
	}

	// Main server - pilot, registries
	server, err := NewServer(args)
	if err != nil {
		return nil, err
	}

	if err := server.WatchMeshConfig(meshCfgFile); err != nil {
		return nil, fmt.Errorf("mesh: %v", err)
	}

	pilotAddress := server.Mesh.DefaultConfig.DiscoveryAddress
	_, port, _ := net.SplitHostPort(pilotAddress)
	basePortI, _ := strconv.Atoi(port)
	basePortI = basePortI - basePortI % 100
	basePort := int32(basePortI)
	server.basePort = basePort


	args.DiscoveryOptions =  envoy.DiscoveryServiceOptions{
		HTTPAddr:        fmt.Sprintf(":%d", basePort+7),
		GrpcAddr:        fmt.Sprintf(":%d", basePort+10),
		SecureGrpcAddr:  "",
		EnableCaching:   true,
		EnableProfiling: true,
	}
	args.CtrlZOptions = &ctrlz.Options{
		Address: "localhost",
		Port:    uint16(basePort + 12),
	}

		err = server.InitConfig()
	if err != nil {
		return nil, err
	}

	// Galley args
	gargs := settings.DefaultArgs()

	// Default dir.
	// If not set, will attempt to use K8S.
	gargs.ConfigPath = baseDir + "/var/lib/istio/local"
	// TODO: load a json file to override defaults (for all components)

	gargs.ValidationArgs.EnableValidation = false
	gargs.ValidationArgs.EnableReconcileWebhookConfiguration = false
	gargs.APIAddress = fmt.Sprintf("tcp://0.0.0.0:%d", basePort+901)
	gargs.Insecure = true
	gargs.EnableServer = true
	gargs.DisableResourceReadyCheck = true
	// Use Galley Ctrlz for all services.
	gargs.IntrospectionOptions.Port = uint16(basePort + 876)

	// The file is loaded and watched by Galley using galley/pkg/meshconfig watcher/reader
	// Current code in galley doesn't expose it - we'll use 2 Caches instead.

	// Defaults are from pkg/config/mesh

	// Actual files are loaded by galley/pkg/src/fs, which recursively loads .yaml and .yml files
	// The files are suing YAMLToJSON, but interpret Kind, APIVersion

	gargs.MeshConfigFile = meshCfgFile
	gargs.MonitoringPort = uint(basePort + 15)
	// Galley component
	// TODO: runs under same gRPC port.
	server.Galley = NewGalleyServer(gargs)

	// TODO: start injection (only for K8S variant)

	// TODO: start envoy only if TLS certs exist (or bootstrap token and SDS server address is configured)
	//err = startEnvoy(baseDir, &mcfg)
	//if err != nil {
	//	return err
	//}
	return server, nil
}

func (s *Server) WaitDrain(baseDir string) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	// Will gradually terminate connections to Pilot
	DrainEnvoy(baseDir, s.Args.MeshConfig.DefaultConfig)

}


var trustDomain = "cluster.local"

// TODO: use pilot-agent code, and refactor it to extract the core functionality.

// TODO: better implementation for 'drainFile' config - used by agent.terminate()

// startEnvoy starts the envoy sidecar for Istio control plane, for TLS and load balancing.
// Should be called after cert generation
func (s *Server) StartEnvoy(baseDir string, mcfg *meshv1.MeshConfig) error {
	os.Mkdir(baseDir+"/etc/istio/proxy", 0700)

	cfg := &meshv1.ProxyConfig{}
	// Copy defaults
	pcval, _ := mcfg.DefaultConfig.Marshal()
	cfg.Unmarshal(pcval)

	// This is the local envoy serving the control plane - gets configs from localhost, no TLS
	cfg.DiscoveryAddress = fmt.Sprintf("localhost:%d", s.basePort+10)

	cfg.ProxyAdminPort =  s.basePort

	// Override shutdown, it's too slow
	cfg.ParentShutdownDuration = types.DurationProto(5 * time.Second)

	cfg.ConfigPath = baseDir + "/etc/istio/proxy"

	// Let's try to use the same bootstrap that sidecars are using - without a special config for istiod.
	// Will use localhost and get configs from pilot.
	cfg.CustomConfigFile =       baseDir + "/var/lib/istio/envoy/envoy_bootstrap_tmpl.json"

	nodeId := "sidecar~127.0.0.1~istio-pilot.istio-system~istio-system.svc.cluster.local"
	env := os.Environ()
	env = append(env, "ISTIO_META_ISTIO_VERSION=1.4")

	//cfgF, err := agent.WriteBootstrap(cfg, nodeId, 1, []string{
	//	"istio-pilot.istio-system",
	//	fmt.Sprintf("spiffe://%s/ns/%s/sa/%s", trustDomain, "istio-system", "istio-pilot-service-account"),
	//},
	//	map[string]interface{}{},
	//	env,
	//	[]string{"127.0.0.1"}, // node IPs
	//	"60s")

	cfgF, err := agent.New(agent.Config{
		Node:                "sidecar~127.0.0.1~istio-pilot.istio-system~istio-system.svc.cluster.local",
		DNSRefreshRate:      "300s",
		Proxy:               cfg,
		PilotSubjectAltName: nil,
		MixerSubjectAltName: nil,
		LocalEnv:            os.Environ(),
		NodeIPs:             []string{"127.0.0.1"},
		PodName:             "istiod",
		PodNamespace:        "istio-system",
		PodIP:               net.IP([]byte{127,0,0,1}),
		SDSUDSPath:          "",
		SDSTokenPath:        "",
		ControlPlaneAuth:    false,
		DisableReportCalls:  true,
	}).CreateFileForEpoch(0)
	if err != nil {
		return err
	}
	log.Println("Created ", cfgF)

	// Start Envoy, using the pre-generated config. No restarts: if it crashes, we exit.
	stop := make(chan error)
	//features.EnvoyBaseId.DefaultValue = "1"
	process, err := agent.RunProxy(cfg, nodeId, 1, cfgF, stop,
		os.Stdout, os.Stderr, []string{
			"--disable-hot-restart",
			// "-l", "trace",
		})
	if err != nil {
		log.Fatal("Failed to start envoy sidecar for istio", err)
	}
	go func() {
		// Should not happen.
		process.Wait()
		log.Fatal("Envoy terminated, restart.")
	}()
	return err
}

// Start the galley component, with its args.

// Galley by default initializes some probes - we'll use Pilot probes instead, since it also checks for galley
// TODO: have  the probes check all other components

// Validation is not included in hyperistio - standalone Galley or external address should be used, it's not
// part of the minimal set.

// Monitoring, profiling are common across all components, skipping as part of Galley startup
// Ctrlz is custom for galley - setting it up here.
