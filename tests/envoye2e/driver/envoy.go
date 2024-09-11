// Copyright 2019 Istio Authors
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

package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/cncf/xds/go/udpa/type/v1" // Preload proto definitions
	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/open_telemetry/v3" // Preload proto definitions
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/http_inspector/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/stat_sinks/open_telemetry/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/internal_upstream/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"

	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/envoye2e/env"
)

// Envoy starts up a Envoy process locally.
type Envoy struct {
	// template for the bootstrap
	Bootstrap string

	// Istio proxy version to download.
	// This could be either a patch version (x.y.z), or a minor version (x.y), or master.
	// When minor version or master is provided, proxy binary will downloaded based on
	// the latest proxy SHA that istio minor version branch points to.
	// DownloadVersion will be ignored if proxy binary already exists at the
	// default location, or ENVOY_PATH env var is set.
	DownloadVersion string

	// standard error for the Envoy process (defaults to os.Stderr).
	Stderr io.Writer
	// standard out for the Envoy process (defaults to os.Stdout).
	Stdout io.Writer

	// Value used to set the --concurrency flag when starting envoy.
	Concurrency uint32

	tmpFile   string
	cmd       *exec.Cmd
	adminPort uint32

	done chan error
}

var _ Step = &Envoy{}

// Run starts a Envoy process.
func (e *Envoy) Run(p *Params) error {
	bootstrap, err := p.Fill(e.Bootstrap)
	if err != nil {
		return err
	}
	log.Printf("envoy bootstrap:\n%s\n", bootstrap)

	var node string
	e.adminPort, node, err = getAdminPortAndNode(bootstrap)
	if err != nil {
		return err
	}
	log.Printf("admin port %d", e.adminPort)

	tmp, err := os.CreateTemp(os.TempDir(), "envoy-bootstrap-*.yaml")
	if err != nil {
		return err
	}
	if _, err := tmp.Write([]byte(bootstrap)); err != nil {
		return err
	}
	e.tmpFile = tmp.Name()

	debugLevel, ok := os.LookupEnv("ENVOY_DEBUG")
	if !ok {
		debugLevel = "info"
	}
	concurrency := "1"
	if e.Concurrency > 1 {
		concurrency = fmt.Sprint(e.Concurrency)
	}
	args := []string{
		"--log-format", "[" + node + ` %T.%e][%t][%l][%n] [%g:%#] %v`,
		"-c", e.tmpFile,
		"-l", debugLevel,
		"--concurrency", concurrency,
		"--disable-hot-restart",
		"--drain-time-s", "4", // this affects how long draining listenrs are kept alive
	}
	envoyPath := filepath.Join(testenv.LocalOut, "envoy")
	if path, exists := os.LookupEnv("ENVOY_PATH"); exists {
		envoyPath = path
	} else if _, err := os.Stat(envoyPath); os.IsNotExist(err) && e.DownloadVersion != "" {
		envoyPath, err = downloadEnvoy(e.DownloadVersion)
		if err != nil {
			return fmt.Errorf("failed to download Envoy binary %v", err)
		}
	}
	stderr := io.Writer(os.Stderr)
	if e.Stderr != nil {
		stderr = e.Stderr
	}
	stdout := io.Writer(os.Stdout)
	if e.Stdout != nil {
		stdout = e.Stdout
	}
	cmd := exec.Command(envoyPath, args...)
	cmd.Stderr = stderr
	cmd.Stdout = stdout
	cmd.Dir = filepath.Join(testenv.IstioSrc, "tests/envoye2e")

	log.Printf("envoy cmd %v", cmd.Args)
	e.cmd = cmd
	if err = cmd.Start(); err != nil {
		return err
	}
	e.done = make(chan error, 1)
	go func() {
		err := e.cmd.Wait()
		if err != nil {
			log.Printf("envoy process error: %v\n", err)
			if strings.Contains(err.Error(), "segmentation fault") {
				panic(err)
			}
		}
		e.done <- err
	}()

	url := fmt.Sprintf("http://127.0.0.1:%v/ready", e.adminPort)
	return env.WaitForHTTPServer(url)
}

// Cleanup stops the Envoy process.
func (e *Envoy) Cleanup() {
	log.Printf("stop envoy ...\n")
	defer os.Remove(e.tmpFile)
	if e.cmd != nil {
		url := fmt.Sprintf("http://127.0.0.1:%v/quitquitquit", e.adminPort)
		_, _, _ = env.HTTPPost(url, "", "")
		select {
		case <-time.After(3 * time.Second):
			log.Println("envoy killed as timeout reached")
			log.Println(e.cmd.Process.Kill())
		case <-e.done:
			log.Printf("stop envoy ... done\n")
		}
	}
}

func getAdminPortAndNode(bootstrap string) (port uint32, node string, err error) {
	pb := &bootstrapv3.Bootstrap{}
	if err = ReadYAML(bootstrap, pb); err != nil {
		return
	}
	if pb.Admin == nil || pb.Admin.Address == nil {
		err = fmt.Errorf("missing admin section in bootstrap: %v", bootstrap)
		return
	}
	socket, ok := pb.Admin.Address.Address.(*core.Address_SocketAddress)
	if !ok {
		err = fmt.Errorf("missing socket in bootstrap: %v", bootstrap)
		return
	}
	portValue, ok := socket.SocketAddress.PortSpecifier.(*core.SocketAddress_PortValue)
	if !ok {
		err = fmt.Errorf("missing port in bootstrap: %v", bootstrap)
		return
	}
	node = pb.Node.Id
	port = portValue.PortValue
	return
}

// downloads env based on the given branch name. Return location of downloaded envoy.
func downloadEnvoy(ver string) (string, error) {
	var proxyDepURL string
	if regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`).MatchString(ver) {
		// this is a patch version string
		proxyDepURL = fmt.Sprintf("https://raw.githubusercontent.com/istio/istio/%v/istio.deps", ver)
	} else if regexp.MustCompile(`[0-9]+\.[0-9]+`).MatchString(ver) {
		// this is a minor version string
		proxyDepURL = fmt.Sprintf("https://raw.githubusercontent.com/istio/istio/release-%v/istio.deps", ver)
	} else if ver == "master" {
		proxyDepURL = "https://raw.githubusercontent.com/istio/istio/master/istio.deps"
	} else {
		return "", fmt.Errorf("envoy version %v is neither minor version nor patch version", ver)
	}
	resp, err := http.Get(proxyDepURL)
	if err != nil {
		return "", fmt.Errorf("cannot get envoy sha from %v: %v", proxyDepURL, err)
	}
	defer resp.Body.Close()
	istioDeps, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("cannot read body of istio deps: %v", err)
	}

	var deps []interface{}
	if err := json.Unmarshal(istioDeps, &deps); err != nil {
		return "", err
	}
	proxySHA := ""
	for _, d := range deps {
		if dm, ok := d.(map[string]interface{}); ok && dm["repoName"].(string) == "proxy" {
			proxySHA = dm["lastStableSHA"].(string)
		}
	}
	if proxySHA == "" {
		return "", errors.New("cannot identify proxy SHA to download")
	}

	dir := fmt.Sprintf("%s/%s", os.TempDir(), "istio-proxy")
	dst := fmt.Sprintf("%v/envoy-%v", dir, proxySHA)
	if _, err := os.Stat(dst); err == nil {
		// If the desired envoy binary is already downloaded, skip downloading and return.
		return dst, nil
	}

	// clean up the tmp dir before downloading to remove stale envoy binary.
	_ = os.RemoveAll(dir)

	// make temp directory to put downloaded envoy binary.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	envoyURL := fmt.Sprintf("https://storage.googleapis.com/istio-build/proxy/envoy-alpha-%v.tar.gz", proxySHA)
	downloadCmd := exec.Command("bash", "-c", fmt.Sprintf("curl -fLSs %v | tar xz", envoyURL))
	downloadCmd.Stderr = os.Stderr
	downloadCmd.Stdout = os.Stdout
	err = downloadCmd.Run()
	if err != nil {
		return "", fmt.Errorf("fail to run envoy download command: %v", err)
	}
	src := "usr/local/bin/envoy"
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("fail to find downloaded envoy: %v", err)
	}
	defer os.RemoveAll("usr/")

	cpCmd := exec.Command("cp", "-n", src, dst)
	cpCmd.Stderr = os.Stderr
	cpCmd.Stdout = os.Stdout
	if err := cpCmd.Run(); err != nil {
		return "", fmt.Errorf("fail to copy envoy binary from %v to %v: %v", src, dst, err)
	}

	return dst, nil
}
