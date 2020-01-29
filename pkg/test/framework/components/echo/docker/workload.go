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

package docker

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"istio.io/istio/pkg/test"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/docker"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/docker/images"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	agentStatusPort = 15020
)

var (
	// TODO(nmittler): Retrieve this from the environment.
	authPolicy = v1alpha1.AuthenticationPolicy_MUTUAL_TLS

	_ echo.Workload   = &workload{}
	_ resource.Dumper = &workload{}
)

type workload struct {
	*client.Instance

	dumpDir string
	portMap *portMap

	container      *docker.Container
	sidecar        *sidecar
	readinessProbe probeFunc
}

func newWorkload(e *native.Environment, cfg echo.Config, dumpDir string) (out *workload, err error) {
	dockerClient, err := e.DockerClient()
	if err != nil {
		return nil, err
	}

	network, err := e.Network()
	if err != nil {
		return nil, err
	}

	// Get the Docker images for the Echo application.
	imgs, err := images.Get()
	if err != nil {
		return nil, err
	}

	w := &workload{
		dumpDir: dumpDir,
	}
	defer func() {
		if err != nil {
			w.Dump()
			_ = w.Close()
		}
	}()

	// Make sure the hostname starts with the service (there are tests that rely on this).
	// Note: not using FQDN here to avoid exceeding the linux limit of 64 characters in sethostname.
	// TODO(nmittler): Consider refactoring tests that rely on hostname to instead use service name.
	hostName := fmt.Sprintf("%s-%s", cfg.Service, uuid.New().String())

	// Create a mapping of container to host ports.
	w.portMap, err = newPortMap(e.PortManager, cfg)
	if err != nil {
		return nil, err
	}

	echoArgs := append([]string{
		"--version", cfg.Version,
	}, w.portMap.toEchoArgs()...)

	var image string
	var cmd []string
	var env []string
	var extraHosts []string
	var capabilities []string
	if cfg.Annotations.GetBool(echo.SidecarInject) {
		w.readinessProbe = sidecarReadinessProbe(w.portMap.hostAgentPort)

		image = imgs.Sidecar

		// Need NET_ADMIN for iptables.
		capabilities = []string{"NET_ADMIN"}

		pilotHost := fmt.Sprintf("istio-pilot.%s", e.SystemNamespace)

		pilotAddress := fmt.Sprintf("%s:%d", pilotHost, discoveryPort(cfg.Pilot))

		// Add hosts entry for Pilot to the container.
		ip, err := getIP()
		if err != nil {
			return nil, err
		}
		extraHosts = []string{
			fmt.Sprintf("%s:%s", pilotHost, ip.String()),
		}

		agentArgs := fmt.Sprintf("--proxyLogLevel debug --statusPort %d --domain %s --trust-domain %s",
			agentStatusPort, e.Domain, e.Domain)

		metaJSONLabels := fmt.Sprintf("{\"app\":\"%s\"}", cfg.Service)
		interceptionMode := "REDIRECT"

		env = append(env,
			"ECHO_ARGS="+strings.Join(echoArgs, " "),
			"ISTIO_SERVICE="+cfg.Service,
			"ISTIO_NAMESPACE="+cfg.Namespace.Name(),
			"ISTIO_SYSTEM_NAMESPACE="+e.SystemNamespace,
			"POD_NAME="+hostName,
			"POD_NAMESPACE="+cfg.Namespace.Name(),
			"PILOT_ADDRESS="+pilotAddress,
			"CITADEL_ADDRESS=istio-citadel:8060", // TODO: need local citadel
			"ENVOY_PORT=15001",
			"ENVOY_USER=istio-proxy",
			"ISTIO_AGENT_FLAGS="+agentArgs,
			"ISTIO_INBOUND_INTERCEPTION_MODE="+interceptionMode,
			"ISTIO_SERVICE_CIDR=*",
			"ISTIO_CP_AUTH="+authPolicy.String(),
			"ISTIO_META_ISTIO_PROXY_SHA="+envoy.LatestStableSHA,
			"ISTIO_META_POD_NAME="+hostName,
			"ISTIO_META_CONFIG_NAMESPACE="+cfg.Namespace.Name(),
			"ISTIO_METAJSON_LABELS="+metaJSONLabels,
			"ISTIO_META_INTERCEPTION_MODE="+interceptionMode,
		)
	} else {
		w.readinessProbe = noSidecarReadinessProbe(w.portMap.http().hostPort)

		image = imgs.NoSidecar

		// Add arguments for the entry point.
		cmd = echoArgs
	}

	// Use the FQDN of the service as the container name.
	containerName := cfg.FQDN()

	// Provide network aliases for this host using either just namespace or FQDN.
	aliases := []string{
		fmt.Sprintf("%s.%s", cfg.Service, cfg.Namespace.Name()),
		cfg.FQDN(),
	}

	containerCfg := docker.ContainerConfig{
		Name:       containerName,
		Network:    network,
		Labels:     network.Labels, // Use the same labels as the network.
		Hostname:   hostName,
		Image:      image,
		CapAdd:     capabilities,
		Aliases:    aliases,
		ExtraHosts: extraHosts,
		PortMap:    w.portMap.toDocker(),
		Cmd:        cmd,
		Env:        env,
	}
	if w.container, err = docker.NewContainer(dockerClient, containerCfg); err != nil {
		return nil, fmt.Errorf("failed creating Docker container: %v\nContainer Config:\n%+v",
			err, containerCfg)
	}

	if cfg.Annotations.GetBool(echo.SidecarInject) {
		if w.sidecar, err = newSidecar(w.container); err != nil {
			return nil, fmt.Errorf("failed creating sidecar for Echo Container %s: %v",
				w.container.Name, err)
		}
	}

	return w, nil
}

func (w *workload) waitForReady() (err error) {
	// Wait until the workload is ready.
	if err := retry.UntilSuccess(w.readinessProbe, retry.Delay(2*time.Second), retry.Timeout(20*time.Second)); err != nil {
		return fmt.Errorf("failed waiting for Echo container %s: %v", w.container.Name, err)
	}

	// Now create the GRPC client.
	if w.Instance, err = client.New(fmt.Sprintf("%s:%d", localhost, w.portMap.grpc().hostPort)); err != nil {
		return fmt.Errorf("failed creating GRPC client for Echo container %s: %v", w.container.Name, err)
	}
	return nil
}

func (w *workload) Close() (err error) {
	if w.Instance != nil {
		err = multierror.Append(err, w.Instance.Close()).ErrorOrNil()
	}
	if w.container != nil {
		err = multierror.Append(err, w.container.Close()).ErrorOrNil()
	}
	return
}

func (w *workload) Address() string {
	return w.container.IPAddress
}

func (w *workload) Sidecar() echo.Sidecar {
	return w.sidecar
}

func (w *workload) Dump() {
	if w.container == nil {
		return
	}

	scopes.CI.Errorf("=== Dumping state for Echo container %s to:\n%s", w.container.Name, w.dumpDir)
	l, err := w.container.Logs()
	if err != nil {
		scopes.CI.Errorf("Unable to get logs for Echo container %s: %v", w.container.Name, err)
	}

	fname := filepath.Join(w.dumpDir, "echo.log")
	if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
		scopes.CI.Errorf("Unable to write logs for Echo container %s: %v", w.container.Name, err)
	}

	if w.sidecar != nil {
		srcFiles := []string{
			"/var/log/istio/istio.log",
			"/var/log/istio/istio.err.log",
		}

		for _, srcFile := range srcFiles {
			result, err := w.container.Exec(context.Background(), "cat", srcFile)
			if err != nil {
				scopes.CI.Errorf("Unable to retrieve %s for Echo container %s: %v", srcFile, w.container.Name, err)
				continue
			}

			base := filepath.Base(srcFile)
			fname = filepath.Join(w.dumpDir, base)
			if err = ioutil.WriteFile(fname, result.StdOut, os.ModePerm); err != nil {
				scopes.CI.Errorf("Unable to write %s for Echo container %s: %v", base, w.container.Name, err)
			}
		}

		// Dump the Envoy config.
		var configDump *envoyAdmin.ConfigDump
		if configDump, err = w.sidecar.Config(); err == nil {
			m := jsonpb.Marshaler{
				Indent: "  ",
			}
			var out string
			if out, err = m.MarshalToString(configDump); err == nil {
				fname := filepath.Join(w.dumpDir, "config_dump.json")
				err = ioutil.WriteFile(fname, []byte(out), os.ModePerm)
			}
		}
		if err != nil {
			scopes.CI.Errorf("Unable to retrieve config_dump for Echo container %s: %v", w.container.Name, err)
		}
	}
}

func discoveryPort(p pilot.Instance) int {
	if authPolicy == v1alpha1.AuthenticationPolicy_MUTUAL_TLS {
		return p.(pilot.Native).GetSecureDiscoveryAddress().Port
	}
	return p.(pilot.Native).GetDiscoveryAddress().Port
}

func (w *workload) Logs() (string, error) {
	return w.container.Logs()
}

func (w *workload) LogsOrFail(t test.Failer) string {
	t.Helper()
	logs, err := w.Logs()
	if err != nil {
		t.Fatal(err)
	}
	return logs
}
