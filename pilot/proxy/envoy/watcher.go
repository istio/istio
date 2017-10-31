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
	"hash"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/howeyc/fsnotify"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/proxy"
)

// Watcher triggers reloads on changes to the proxy config
type Watcher interface {
	// Run the watcher loop (blocking call)
	Run(context.Context)

	// Reload the agent with the latest configuration
	Reload()
}

// CertSource is file source for certificates
type CertSource struct {
	// Directory containing certificates
	Directory string
	// Files for certificates
	Files []string
}

type watcher struct {
	agent    proxy.Agent
	role     proxy.Node
	config   proxyconfig.ProxyConfig
	certs    []CertSource
	pilotSAN []string
}

// NewWatcher creates a new watcher instance from a proxy agent and a set of monitored certificate paths
// (directories with files in them)
func NewWatcher(config proxyconfig.ProxyConfig, agent proxy.Agent, role proxy.Node,
	certs []CertSource, pilotSAN []string) Watcher {
	return &watcher{
		agent:    agent,
		role:     role,
		config:   config,
		certs:    certs,
		pilotSAN: pilotSAN,
	}
}

// defaultMinDelay is the minimum amount of time between delivery of two successive events via updateFunc.
const defaultMinDelay = 10 * time.Second

func (w *watcher) Run(ctx context.Context) {
	// agent consumes notifications from the controller
	go w.agent.Run(ctx)

	// kickstart the proxy with partial state (in case there are no notifications coming)
	w.Reload()

	// monitor certificates
	certDirs := make([]string, 0, len(w.certs))
	for _, cert := range w.certs {
		certDirs = append(certDirs, cert.Directory)
	}

	go watchCerts(ctx, certDirs, watchFileEvents, defaultMinDelay, w.Reload)

	<-ctx.Done()
}

func (w *watcher) Reload() {
	config := buildConfig(w.config, w.pilotSAN)

	// compute hash of dependent certificates
	h := sha256.New()
	for _, cert := range w.certs {
		generateCertHash(h, cert.Directory, cert.Files)
	}
	config.Hash = h.Sum(nil)

	w.agent.ScheduleConfigUpdate(config)
}

type watchFileEventsFn func(ctx context.Context, wch <-chan *fsnotify.FileEvent,
	minDelay time.Duration, notifyFn func())

// watchFileEvents watches for changes on a channel and notifies via notifyFn().
// The function batches changes so that related changes are processed together.
// The function ensures that notifyFn() is called no more than one time per minDelay.
// The function does not return until the the context is cancelled.
func watchFileEvents(ctx context.Context, wch <-chan *fsnotify.FileEvent, minDelay time.Duration, notifyFn func()) {
	// timer and channel for managing minDelay.
	var timeChan <-chan time.Time
	var timer *time.Timer

	for {
		select {
		case ev := <-wch:
			glog.Infof("watchFileEvents: %s", ev.String())
			if timer != nil {
				continue
			}
			// create new timer
			timer = time.NewTimer(minDelay)
			timeChan = timer.C
		case <-timeChan:
			// reset timer
			timeChan = nil
			timer.Stop()
			timer = nil

			glog.Info("watchFileEvents: notifying")
			notifyFn()
		case <-ctx.Done():
			glog.V(2).Info("watchFileEvents has terminated")
			return
		}
	}
}

// watchCerts watches all certificate directories and calls the provided
// `updateFunc` method when changes are detected. This method is blocking
// so it should be run as a goroutine.
// updateFunc will not be called more than one time per minDelay.
func watchCerts(ctx context.Context, certsDirs []string, watchFileEventsFn watchFileEventsFn,
	minDelay time.Duration, updateFunc func()) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		glog.Warningf("failed to create a watcher for certificate files: %v", err)
		return
	}
	defer func() {
		if err := fw.Close(); err != nil {
			glog.Warningf("closing watcher encounters an error %v", err)
		}
	}()

	// watch all directories
	for _, d := range certsDirs {
		if err := fw.Watch(d); err != nil {
			glog.Warningf("watching %s encounters an error %v", d, err)
			return
		}
	}
	watchFileEventsFn(ctx, fw.Event, minDelay, updateFunc)
}

func generateCertHash(h hash.Hash, certsDir string, files []string) {
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		return
	}

	for _, file := range files {
		filename := path.Join(certsDir, file)
		bs, err := ioutil.ReadFile(filename)
		if err != nil {
			// glog.Warningf("failed to read file %q", filename)
			continue
		}
		if _, err := h.Write(bs); err != nil {
			glog.Warning(err)
		}
	}
}

const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "envoy-rev%d.json"
)

func configFile(config string, epoch int) string {
	return path.Join(config, fmt.Sprintf(EpochFileTemplate, epoch))
}

type envoy struct {
	config    proxyconfig.ProxyConfig
	node      string
	extraArgs []string
}

// NewProxy creates an instance of the proxy control commands
func NewProxy(config proxyconfig.ProxyConfig, node string) proxy.Proxy {
	// inject tracing flag for higher levels
	var args []string
	if glog.V(4) {
		args = append(args, "-l", "trace")
	} else if glog.V(3) {
		args = append(args, "-l", "debug")
	}

	return envoy{
		config:    config,
		node:      node,
		extraArgs: args,
	}
}

func (proxy envoy) args(fname string, epoch int) []string {
	startupArgs := []string{"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(proxy.config.DrainDuration) / time.Second)),
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(proxy.config.ParentShutdownDuration) / time.Second)),
		"--service-cluster", proxy.config.ServiceCluster,
		"--service-node", proxy.node,
	}

	startupArgs = append(startupArgs, proxy.extraArgs...)

	if len(proxy.config.AvailabilityZone) > 0 {
		startupArgs = append(startupArgs, []string{"--service-zone", proxy.config.AvailabilityZone}...)
	}

	return startupArgs
}

func (proxy envoy) Run(config interface{}, epoch int, abort <-chan error) error {
	envoyConfig, ok := config.(*Config)
	if !ok {
		return fmt.Errorf("unexpected config type: %#v", config)
	}

	var fname string
	// Note: the cert checking still works, the generated file is updated if certs are changed.
	// We just don't save the generated file, but use a custom one instead. Pilot will keep
	// monitoring the certs and restart if the content of the certs changes.
	if len(proxy.config.CustomConfigFile) > 0 {
		// there is a custom configuration. Don't write our own config - but keep watching the certs.
		fname = proxy.config.CustomConfigFile
	} else {
		// create parent directories if necessary
		if err := os.MkdirAll(proxy.config.ConfigPath, 0700); err != nil {
			return multierror.Prefix(err, "failed to create directory for proxy configuration")
		}

		// attempt to write file
		fname = configFile(proxy.config.ConfigPath, epoch)
		if err := envoyConfig.WriteFile(fname); err != nil {
			return err
		}
	}

	// spin up a new Envoy process
	args := proxy.args(fname, epoch)

	glog.V(2).Infof("Envoy command: %v", args)

	/* #nosec */
	cmd := exec.Command(proxy.config.BinaryPath, args...)
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
	path := configFile(proxy.config.ConfigPath, epoch)
	if err := os.Remove(path); err != nil {
		glog.Warningf("Failed to delete config file %s for %d, %v", path, epoch, err)
	}
}

func (proxy envoy) Panic(_ interface{}) {
	glog.Fatal("cannot start the proxy with the desired configuration")
}
