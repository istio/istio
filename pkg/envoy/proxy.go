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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"google.golang.org/protobuf/types/known/durationpb"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/env"
	common_features "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/log"
)

type envoy struct {
	ProxyConfig
	extraArgs []string
}

// Envoy binary flags
type ProxyConfig struct {
	LogLevel           string
	ComponentLogLevel  string
	NodeIPs            []string
	Sidecar            bool
	LogAsJSON          bool
	OutlierLogPath     string
	SkipDeprecatedLogs bool

	BinaryPath    string
	ConfigPath    string
	ConfigCleanup bool
	AdminPort     int32
	DrainDuration *durationpb.Duration
	Concurrency   int32

	// For unit testing, in combination with NoEnvoy prevents agent.Run from blocking
	TestOnly    bool
	AgentIsRoot bool
}

// NewProxy creates an instance of the proxy control commands
func NewProxy(cfg ProxyConfig) Proxy {
	// inject tracing flag for higher levels
	var args []string
	logLevel, componentLogs := splitComponentLog(cfg.LogLevel)
	if logLevel != "" {
		args = append(args, "-l", logLevel)
	}
	if len(componentLogs) > 0 {
		args = append(args, "--component-log-level", strings.Join(componentLogs, ","))
	} else if cfg.ComponentLogLevel != "" {
		// Use the old setting if we don't set any component log levels in LogLevel
		args = append(args, "--component-log-level", cfg.ComponentLogLevel)
	}

	if cfg.SkipDeprecatedLogs {
		args = append(args, "--skip-deprecated-logs")
	}

	// Explicitly enable core dumps. This may be desirable more often (by default), but for now we only set it in VM tests.
	if enableEnvoyCoreDump {
		args = append(args, "--enable-core-dump")
	}
	return &envoy{
		ProxyConfig: cfg,
		extraArgs:   args,
	}
}

// splitComponentLog breaks down an argument string into a log level (ie "info") and component log levels (ie "misc:error").
// This allows using a single log level API, with the same semantics as Istio's logging, to configure Envoy which
// has two different settings
func splitComponentLog(level string) (string, []string) {
	levels := strings.Split(level, ",")
	var logLevel string
	var componentLogs []string
	for _, sl := range levels {
		spl := strings.Split(sl, ":")
		if len(spl) == 1 {
			logLevel = spl[0]
		} else if len(spl) == 2 {
			componentLogs = append(componentLogs, sl)
		} else {
			log.Warnf("dropping invalid log level: %v", sl)
		}
	}
	return logLevel, componentLogs
}

func (e *envoy) Drain(skipExit bool) error {
	adminPort := uint32(e.AdminPort)

	err := DrainListeners(adminPort, e.Sidecar, skipExit)
	if err != nil {
		log.Infof("failed draining listeners for Envoy on port %d: %v", adminPort, err)
	}
	return err
}

func (e *envoy) UpdateConfig(config []byte) error {
	return os.WriteFile(e.ConfigPath, config, 0o666)
}

func (e *envoy) args(fname string, overrideFname string) []string {
	proxyLocalAddressType := "v4"
	if network.AllIPv6(e.NodeIPs) {
		proxyLocalAddressType = "v6"
	}
	startupArgs := []string{
		"-c", fname,
		"--drain-time-s", fmt.Sprint(int(e.DrainDuration.AsDuration().Seconds())),
		"--drain-strategy", "immediate", // Clients are notified as soon as the drain process starts.
		"--local-address-ip-version", proxyLocalAddressType,
		// Reduce default flush interval from 10s to 1s. The access log buffer size is 64k and each log is ~256 bytes
		// This means access logs will be written once we have ~250 requests, or ever 1s, which ever comes first.
		// Reducing this to 1s optimizes for UX while retaining performance.
		// At low QPS access logs are unlikely a bottleneck, and these users will now see logs after 1s rather than 10s.
		// At high QPS (>250 QPS) we will log the same amount as we will log due to exceeding buffer size, rather
		// than the flush interval.
		"--file-flush-interval-msec", "1000",
		"--disable-hot-restart", // We don't use it, so disable it to simplify Envoy's logic
		"--allow-unknown-static-fields",
	}

	startupArgs = append(startupArgs, e.extraArgs...)

	if overrideFname != "" {
		s, err := readBootstrapToJSON(overrideFname)
		if err != nil {
			log.Warnf("Failed to read bootstrap override: %v", err)
		} else {
			// Despite the name Envoy also accepts JSON string
			startupArgs = append(startupArgs, "--config-yaml", s)
		}
	}

	if e.Concurrency > 0 {
		startupArgs = append(startupArgs, "--concurrency", fmt.Sprint(e.Concurrency))
	}

	return startupArgs
}

// readBootstrapToJSON reads a config file, in YAML or JSON, and returns JSON string
func readBootstrapToJSON(fname string) (string, error) {
	b, err := os.ReadFile(fname)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %s, %v", fname, err)
	}

	// Replace host with HOST_IP env var if it is "$(HOST_IP)".
	// This is to support some tracer setting (Datadog, Zipkin), where "$(HOST_IP)" is used for address.
	HostIPEnv := os.Getenv("HOST_IP")

	if strings.Contains(HostIPEnv, ":") { // For IPv6, address needs to be of form `[ff06::c3]:8126`
		HostIPEnv = "[" + HostIPEnv + "]"
		// Avoid adding extra [] where users add them explicitly
		b = bytes.ReplaceAll(b, []byte("[$(HOST_IP)]"), []byte(HostIPEnv))
	}
	b = bytes.ReplaceAll(b, []byte("$(HOST_IP)"), []byte(HostIPEnv))

	converted, err := yaml.YAMLToJSON(b)
	if err != nil {
		return "", fmt.Errorf("failed to convert to JSON: %s, %v", fname, err)
	}
	return string(converted), nil
}

var (
	istioBootstrapOverrideVar = env.Register("ISTIO_BOOTSTRAP_OVERRIDE", "", "")
	enableEnvoyCoreDump       = env.Register("ISTIO_ENVOY_ENABLE_CORE_DUMP", false, "").Get()
)

func (e *envoy) Run(abort <-chan error) error {
	// spin up a new Envoy process
	args := e.args(e.ConfigPath, istioBootstrapOverrideVar.Get())
	log.Infof("Envoy command: %v", args)

	/* #nosec */
	cmd := exec.Command(e.BinaryPath, args...)
	cmd.Env = os.Environ()
	switch common_features.CompliancePolicy {
	// Limit the cipher suites in google_grpc client in Envoy to the compliant ciphers.
	case common_features.FIPS_140_2:
		cmd.Env = append(cmd.Env,
			"GRPC_SSL_CIPHER_SUITES=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384")
	case common_features.FIPS_140_3:
		cmd.Env = append(cmd.Env,
			"GRPC_SSL_CIPHER_SUITES=TLS_AES_256_GCM_SHA384:TLS_AES_128_GCM_SHA256")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e.AgentIsRoot {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: 1337,
			Gid: 1337,
		}
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-abort:
		log.Warnf("Aborting proxy")
		if errKill := cmd.Process.Kill(); errKill != nil {
			log.Warnf("killing proxy caused an error %v", errKill)
		}
		return err
	case err := <-done:
		return err
	}
}

func (e *envoy) Cleanup() {
	if e.ConfigCleanup {
		if err := os.Remove(e.ConfigPath); err != nil {
			log.Warnf("Failed to delete config file %s: %v", e.ConfigPath, err)
		}
	}
}
