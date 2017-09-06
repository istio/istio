// Copyright 2017 Istio Authors
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
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/golang/glog"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/proxy"
)

// Watcher triggers reloads on changes to the proxy config
type Watcher interface {
	// Run the watcher loop (blocking call)
	Run(context.Context)

	// Reload the agent with the latest configuration
	Reload()
}

type watcher struct {
	agent proxy.Agent
	role  proxy.Node
	mesh  *proxyconfig.ProxyMeshConfig
}

// NewWatcher creates a new watcher instance with an agent
func NewWatcher(mesh *proxyconfig.ProxyMeshConfig, agent proxy.Agent, role proxy.Node) (Watcher, error) {
	if mesh.StatsdUdpAddress != "" {
		if addr, err := resolveStatsdAddr(mesh.StatsdUdpAddress); err == nil {
			mesh.StatsdUdpAddress = addr
		} else {
			return nil, err
		}
	}

	if role.Type == proxy.Egress && mesh.EgressProxyAddress == "" {
		return nil, errors.New("egress proxy requires address configuration")
	}

	if role.Type == proxy.Ingress && mesh.IngressControllerMode == proxyconfig.ProxyMeshConfig_OFF {
		return nil, errors.New("ingress proxy is disabled")
	}

	out := &watcher{
		agent: agent,
		role:  role,
		mesh:  mesh,
	}

	return out, nil
}

func (w *watcher) Run(ctx context.Context) {
	// agent consumes notifications from the controllerr
	go w.agent.Run(ctx)

	// kickstart the proxy with partial state (in case there are no notifications coming)
	w.Reload()

	// monitor auth certificates
	if w.mesh.AuthPolicy == proxyconfig.ProxyMeshConfig_MUTUAL_TLS {
		go watchCerts(ctx, w.mesh.AuthCertsPath, w.Reload)
	}

	// monitor ingress certificates
	if w.role.Type == proxy.Ingress {
		go watchCerts(ctx, proxy.IngressCertsPath, w.Reload)
	}

	<-ctx.Done()
}

func (w *watcher) Reload() {
	// use LDS instead of static listeners and clusters
	config := buildConfig(Listeners{}, Clusters{}, true, w.mesh)

	h := sha256.New()
	if w.mesh.AuthPolicy == proxyconfig.ProxyMeshConfig_MUTUAL_TLS {
		generateCertHash(h, w.mesh.AuthCertsPath, authFiles)
	}
	if w.role.Type == proxy.Ingress {
		generateCertHash(h, proxy.IngressCertsPath, []string{"tls.crt", "tls.key"})
	}
	config.Hash = h.Sum(nil)

	w.agent.ScheduleConfigUpdate(config)
}

const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "envoy-rev%d.json"
)

func configFile(config string, epoch int) string {
	return path.Join(config, fmt.Sprintf(EpochFileTemplate, epoch))
}

type envoy struct {
	mesh           *proxyconfig.ProxyMeshConfig
	serviceCluster string
	serviceNode    string
	configpath     string
	binarypath     string
}

// MakeProxy creates an instance of the proxy control commands
func MakeProxy(mesh *proxyconfig.ProxyMeshConfig,
	serviceCluster, serviceNode, configPath, binaryPath string) proxy.Proxy {
	return envoy{
		mesh:           mesh,
		serviceCluster: serviceCluster,
		serviceNode:    serviceNode,
		configpath:     configPath,
		binarypath:     binaryPath,
	}
}

func (proxy envoy) args(fname string, epoch int) []string {
	return []string{"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(proxy.mesh.DrainDuration) / time.Second)),
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(proxy.mesh.ParentShutdownDuration) / time.Second)),
		"--service-cluster", proxy.serviceCluster,
		"--service-node", proxy.serviceNode,
	}
}

func (proxy envoy) Run(config interface{}, epoch int, abort <-chan error) error {
	envoyConfig, ok := config.(*Config)
	if !ok {
		return fmt.Errorf("Unexpected config type: %#v", config)
	}

	// attempt to write file
	fname := configFile(proxy.configpath, epoch)
	if err := envoyConfig.WriteFile(fname); err != nil {
		return err
	}

	// spin up a new Envoy process
	args := proxy.args(fname, epoch)

	// inject tracing flag for higher levels
	if glog.V(4) {
		args = append(args, "-l", "trace")
	} else if glog.V(3) {
		args = append(args, "-l", "debug")
	}

	glog.V(2).Infof("Envoy command: %v", args)

	/* #nosec */
	cmd := exec.Command(proxy.binarypath, args...)
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
		glog.Warningf("Aborting epoch %d", epoch)
		if errKill := cmd.Process.Kill(); errKill != nil {
			glog.Warningf("killing epoch %d caused an error %v", epoch, errKill)
		}
		return err
	case err := <-done:
		return err
	}
}

func (proxy envoy) Cleanup(epoch int) {
	path := configFile(proxy.configpath, epoch)
	if err := os.Remove(path); err != nil {
		glog.Warningf("Failed to delete config file %s for %d, %v", path, epoch, err)
	}
}

func (proxy envoy) Panic(_ interface{}) {
	glog.Fatal("cannot start the proxy with the desired configuration")
}
