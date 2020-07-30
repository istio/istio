// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package envoy

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/env"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/bootstrap"
)

const (
	// epochFileTemplate is a template for the root config JSON
	epochFileTemplate = "envoy-rev%d.json"
)

type envoy struct {
	ProxyConfig
	extraArgs []string
}

type ProxyConfig struct {
	Config              meshconfig.ProxyConfig
	Node                string
	LogLevel            string
	ComponentLogLevel   string
	PilotSubjectAltName []string
	MixerSubjectAltName []string
	NodeIPs             []string
	PodName             string
	PodNamespace        string
	PodIP               net.IP
	STSPort             int
	ControlPlaneAuth    bool
	DisableReportCalls  bool
	OutlierLogPath      string
	PilotCertProvider   string
	ProvCert            string
	Sidecar             bool
}

// NewProxy creates an instance of the proxy control commands
func NewProxy(cfg ProxyConfig) Proxy {
	// inject tracing flag for higher levels
	var args []string
	if cfg.LogLevel != "" {
		args = append(args, "-l", cfg.LogLevel)
	}
	if cfg.ComponentLogLevel != "" {
		args = append(args, "--component-log-level", cfg.ComponentLogLevel)
	}

	return &envoy{
		ProxyConfig: cfg,
		extraArgs:   args,
	}
}

func (e *envoy) IsLive() bool {
	adminPort := uint32(e.Config.ProxyAdminPort)
	info, err := GetServerInfo(adminPort)
	if err != nil {
		log.Infof("failed retrieving server from Envoy on port %d: %v", adminPort, err)
		return false
	}

	if info.State == envoyAdmin.ServerInfo_LIVE {
		// It's live.
		return true
	}

	log.Infof("envoy server not yet live, state: %s", info.State.String())
	return false
}

func (e *envoy) Drain() error {
	adminPort := uint32(e.Config.ProxyAdminPort)

	err := DrainListeners(adminPort, e.Sidecar)
	if err != nil {
		log.Infof("failed draining listeners for Envoy on port %d: %v", adminPort, err)
	}
	return err
}

func (e *envoy) args(fname string, epoch int, bootstrapConfig string) []string {
	proxyLocalAddressType := "v4"
	if isIPv6Proxy(e.NodeIPs) {
		proxyLocalAddressType = "v6"
	}
	startupArgs := []string{"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(e.Config.DrainDuration) / time.Second)),
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(e.Config.ParentShutdownDuration) / time.Second)),
		"--service-cluster", e.Config.ServiceCluster,
		"--service-node", e.Node,
		"--local-address-ip-version", proxyLocalAddressType,
		"--log-format-prefix-with-location", "0",
		// format is like `2020-04-07T16:52:30.471425Z     info    envoy config   ...message..
		// this matches Istio log format
		"--log-format", "%Y-%m-%dT%T.%fZ\t%l\tenvoy %n\t%v",
	}

	startupArgs = append(startupArgs, e.extraArgs...)

	if bootstrapConfig != "" {
		bytes, err := ioutil.ReadFile(bootstrapConfig)
		if err != nil {
			log.Warnf("Failed to read bootstrap override %s, %v", bootstrapConfig, err)
		} else {
			startupArgs = append(startupArgs, "--config-yaml", string(bytes))
		}
	}

	if e.Config.Concurrency.GetValue() > 0 {
		startupArgs = append(startupArgs, "--concurrency", fmt.Sprint(e.Config.Concurrency.GetValue()))
	}

	return startupArgs
}

var istioBootstrapOverrideVar = env.RegisterStringVar("ISTIO_BOOTSTRAP_OVERRIDE", "", "")

func (e *envoy) Run(config interface{}, epoch int, abort <-chan error) error {
	var fname string
	// Note: the cert checking still works, the generated file is updated if certs are changed.
	// We just don't save the generated file, but use a custom one instead. Pilot will keep
	// monitoring the certs and restart if the content of the certs changes.
	if len(e.Config.CustomConfigFile) > 0 {
		// there is a custom configuration. Don't write our own config - but keep watching the certs.
		fname = e.Config.CustomConfigFile
	} else {
		discHost := strings.Split(e.Config.DiscoveryAddress, ":")[0]
		out, err := bootstrap.New(bootstrap.Config{
			Node:                e.Node,
			Proxy:               &e.Config,
			PilotSubjectAltName: e.PilotSubjectAltName,
			MixerSubjectAltName: e.MixerSubjectAltName,
			LocalEnv:            os.Environ(),
			NodeIPs:             e.NodeIPs,
			PodName:             e.PodName,
			PodNamespace:        e.PodNamespace,
			PodIP:               e.PodIP,
			STSPort:             e.STSPort,
			ControlPlaneAuth:    e.ControlPlaneAuth,
			DisableReportCalls:  e.DisableReportCalls,
			OutlierLogPath:      e.OutlierLogPath,
			PilotCertProvider:   e.PilotCertProvider,
			ProvCert:            e.ProvCert,
			DiscoveryHost:       discHost,
		}).CreateFileForEpoch(epoch)
		if err != nil {
			log.Errora("Failed to generate bootstrap config: ", err)
			os.Exit(1) // Prevent infinite loop attempting to write the file, let k8s/systemd report
		}
		fname = out
	}

	// spin up a new Envoy process
	args := e.args(fname, epoch, istioBootstrapOverrideVar.Get())
	log.Infof("Envoy command: %v", args)

	/* #nosec */
	cmd := exec.Command(e.Config.BinaryPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-abort:
		log.Warnf("Aborting epoch %d", epoch)
		if errKill := cmd.Process.Kill(); errKill != nil {
			log.Warnf("killing epoch %d caused an error %v", epoch, errKill)
		}
		return err
	case err := <-done:
		return err
	}
}

func (e *envoy) Cleanup(epoch int) {
	// should return when use the parameter "--templateFile=/path/xxx.tmpl".
	if e.Config.CustomConfigFile != "" {
		return
	}
	filePath := configFile(e.Config.ConfigPath, epoch)
	if err := os.Remove(filePath); err != nil {
		log.Warnf("Failed to delete config file %s for %d, %v", filePath, epoch, err)
	}
}

// convertDuration converts to golang duration and logs errors
func convertDuration(d *types.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, err := types.DurationFromProto(d)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
	}
	return dur
}

func configFile(config string, epoch int) string {
	return path.Join(config, fmt.Sprintf(epochFileTemplate, epoch))
}

// isIPv6Proxy check the addresses slice and returns true for a valid IPv6 address
// for all other cases it returns false
func isIPv6Proxy(ipAddrs []string) bool {
	for i := 0; i < len(ipAddrs); i++ {
		addr := net.ParseIP(ipAddrs[i])
		if addr == nil {
			// Should not happen, invalid IP in proxy's IPAddresses slice should have been caught earlier,
			// skip it to prevent a panic.
			continue
		}
		if addr.To4() != nil {
			return false
		}
	}
	return true
}
