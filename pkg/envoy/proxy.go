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
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
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
	*model.Node
	LogLevel          string
	ComponentLogLevel string
	NodeIPs           []string
	Sidecar           bool
	LogAsJSON         bool
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

func (e *envoy) Drain() error {
	adminPort := uint32(e.Metadata.ProxyConfig.ProxyAdminPort)

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
	startupArgs := []string{
		"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(e.Metadata.ProxyConfig.DrainDuration) / time.Second)),
		"--drain-strategy", "immediate", // Clients are notified as soon as the drain process starts.
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(e.Metadata.ProxyConfig.ParentShutdownDuration) / time.Second)),
		"--service-cluster", e.Metadata.ProxyConfig.ServiceCluster,
		"--service-node", e.ID,
		"--local-address-ip-version", proxyLocalAddressType,
		"--bootstrap-version", "3",
		"--disable-hot-restart", // We don't use it, so disable it to simplify Envoy's logic
	}
	if e.ProxyConfig.LogAsJSON {
		startupArgs = append(startupArgs,
			"--log-format",
			`{"level":"%l","time":"%Y-%m-%dT%T.%fZ","scope":"envoy %n","msg":"%j"}`,
		)
	} else {
		// format is like `2020-04-07T16:52:30.471425Z     info    envoy config   ...message..
		// this matches Istio log format
		startupArgs = append(startupArgs, "--log-format", "%Y-%m-%dT%T.%fZ\t%l\tenvoy %n\t%v")
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

	if e.Metadata.ProxyConfig.Concurrency.GetValue() > 0 {
		startupArgs = append(startupArgs, "--concurrency", fmt.Sprint(e.Metadata.ProxyConfig.Concurrency.GetValue()))
	}

	return startupArgs
}

var istioBootstrapOverrideVar = env.RegisterStringVar("ISTIO_BOOTSTRAP_OVERRIDE", "", "")

func (e *envoy) Run(epoch int, abort <-chan error) error {
	config := e.Metadata.ProxyConfig
	var fname string
	// Note: the cert checking still works, the generated file is updated if certs are changed.
	// We just don't save the generated file, but use a custom one instead. Pilot will keep
	// monitoring the certs and restart if the content of the certs changes.
	if len(config.CustomConfigFile) > 0 {
		// there is a custom configuration. Don't write our own config - but keep watching the certs.
		fname = config.CustomConfigFile
	} else {
		out, err := bootstrap.New(bootstrap.Config{
			Node: e.Node,
		}).CreateFileForEpoch(epoch)
		if err != nil {
			log.Error("Failed to generate bootstrap config: ", err)
			os.Exit(1) // Prevent infinite loop attempting to write the file, let k8s/systemd report
		}
		fname = out
	}

	// spin up a new Envoy process
	args := e.args(fname, epoch, istioBootstrapOverrideVar.Get())
	log.Infof("Envoy command: %v", args)

	/* #nosec */
	cmd := exec.Command(config.BinaryPath, args...)
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
	if e.Metadata.ProxyConfig.CustomConfigFile != "" {
		return
	}
	filePath := configFile(e.Metadata.ProxyConfig.ConfigPath, epoch)
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
