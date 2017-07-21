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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
)

// Watcher triggers reloads on changes to the proxy config
type Watcher interface {
	// Run the watcher loop (blocking call)
	Run(context.Context)

	// Reload the agent with the latest configuration
	Reload()
}

const (
	// IngressCertsPath is the path location for ingress certificates
	IngressCertsPath = "/etc/ingress/"
)

type watcher struct {
	agent   proxy.Agent
	role    proxy.Role
	mesh    *proxyconfig.ProxyMeshConfig
	secrets model.SecretRegistry
}

// NewWatcher creates a new watcher instance with an agent
func NewWatcher(mesh *proxyconfig.ProxyMeshConfig, role proxy.Role, secrets model.SecretRegistry) Watcher {
	glog.V(2).Infof("Proxy role: %#v", role)

	if mesh.StatsdUdpAddress != "" {
		if addr, err := resolveStatsdAddr(mesh.StatsdUdpAddress); err == nil {
			mesh.StatsdUdpAddress = addr
		} else {
			glog.Warningf("Error resolving statsd address; clearing to prevent bad config: %v", err)
			mesh.StatsdUdpAddress = ""
		}
	}

	agent := proxy.NewAgent(runEnvoy(mesh, role.ServiceNode()), proxy.DefaultRetry)
	out := &watcher{
		agent:   agent,
		role:    role,
		mesh:    mesh,
		secrets: secrets,
	}

	return out
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
	if w.role.ServiceNode() == proxy.IngressNode {
		go watchCerts(ctx, IngressCertsPath, w.Reload)

		// update secrets with polling
		go func() {
			for {
				err := w.UpdateIngressSecret(ctx)
				if err != nil {
					glog.Warning(err)
				}

				select {
				case <-time.After(convertDuration(w.mesh.DiscoveryRefreshDelay)):
					// try again
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	<-ctx.Done()
}

func (w *watcher) Reload() {
	config := buildConfig(Listeners{}, Clusters{}, true, w.mesh)

	h := sha256.New()
	if w.mesh.AuthPolicy == proxyconfig.ProxyMeshConfig_MUTUAL_TLS {
		generateCertHash(h, w.mesh.AuthCertsPath, authFiles)
	}
	if w.role.ServiceNode() == proxy.IngressNode {
		generateCertHash(h, IngressCertsPath, []string{"tls.crt", "tls.key"})
	}
	config.Hash = h.Sum(nil)

	w.agent.ScheduleConfigUpdate(config)
}

// UpdateIngressSecret fetches the TLS secret from discovery and secret storage
// and writes to well-known location
func (w *watcher) UpdateIngressSecret(ctx context.Context) error {
	client := &http.Client{Timeout: convertDuration(w.mesh.ConnectTimeout)}
	url := fmt.Sprintf("http://%s/v1alpha/secret/%s/%s",
		w.mesh.DiscoveryAddress, w.mesh.IstioServiceCluster, w.role.ServiceNode())
	req, err := http.NewRequest("GET", url, nil)

	if err != nil {
		return multierror.Prefix(err, "failed to create a request to "+url)
	}

	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return multierror.Prefix(err, "failed to fetch "+url)
	}

	uri, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close() // nolint: errcheck
	if err != nil {
		return multierror.Prefix(err, "failed to read request body")
	}

	secret := string(uri)
	if secret == "" {
		glog.V(4).Info("no secret needed")
		return nil
	}

	tls, err := w.secrets.GetTLSSecret(secret)
	if err != nil {
		return multierror.Prefix(err, "failed to read secret from storage")
	}

	if _, err := os.Stat(IngressCertsPath); os.IsNotExist(err) {
		err = os.Mkdir(IngressCertsPath, 0755)
		if err != nil {
			return multierror.Prefix(err, "cannot create parent directory")
		}
	}

	if err := ioutil.WriteFile(path.Join(IngressCertsPath, "tls.crt"), tls.Certificate, 0755); err != nil {
		return multierror.Prefix(err, "failed to write cert file")
	}
	if err := ioutil.WriteFile(path.Join(IngressCertsPath, "tls.key"), tls.PrivateKey, 0755); err != nil {
		return multierror.Prefix(err, "failed to write key file")
	}

	return nil
}

const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "%s/envoy-rev%d.json"

	// BinaryPath is the path to envoy binary
	BinaryPath = "/usr/local/bin/envoy"

	// ConfigPath is the directory to hold enovy epoch configurations
	ConfigPath = "/etc/envoy"
)

func configFile(config string, epoch int) string {
	return fmt.Sprintf(EpochFileTemplate, config, epoch)
}

func envoyArgs(fname string, epoch int, mesh *proxyconfig.ProxyMeshConfig, node string) []string {
	return []string{"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(mesh.DrainDuration) / time.Second)),
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(mesh.ParentShutdownDuration) / time.Second)),
		"--service-cluster", mesh.IstioServiceCluster,
		"--service-node", node,
	}
}

func runEnvoy(mesh *proxyconfig.ProxyMeshConfig, node string) proxy.Proxy {
	return proxy.Proxy{
		Run: func(config interface{}, epoch int, abort <-chan error) error {
			envoyConfig, ok := config.(*Config)
			if !ok {
				return fmt.Errorf("Unexpected config type: %#v", config)
			}

			// attempt to write file
			fname := configFile(ConfigPath, epoch)
			if err := envoyConfig.WriteFile(fname); err != nil {
				return err
			}

			// spin up a new Envoy process
			args := envoyArgs(fname, epoch, mesh, node)

			// inject tracing flag for higher levels
			if glog.V(4) {
				args = append(args, "-l", "trace")
			} else if glog.V(3) {
				args = append(args, "-l", "debug")
			}

			glog.V(2).Infof("Envoy command: %v", args)

			/* #nosec */
			cmd := exec.Command(BinaryPath, args...)
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
		},
		Cleanup: func(epoch int) {
			path := configFile(ConfigPath, epoch)
			if err := os.Remove(path); err != nil {
				glog.Warningf("Failed to delete config file %s for %d, %v", path, epoch, err)
			}
		},
		Panic: func(_ interface{}) {
			glog.Fatal("cannot start the proxy with the desired configuration")
		},
	}
}
