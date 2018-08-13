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

	"github.com/gogo/protobuf/types"
	"github.com/howeyc/fsnotify"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/log"
)

const (
	// MaxClusterNameLength is the maximum cluster name length
	MaxClusterNameLength = 189 // TODO: use MeshConfig.StatNameLength instead
)

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
	role     model.Proxy
	config   meshconfig.ProxyConfig
	certs    []CertSource
	pilotSAN []string
}

// NewWatcher creates a new watcher instance from a proxy agent and a set of monitored certificate paths
// (directories with files in them)
func NewWatcher(config meshconfig.ProxyConfig, agent proxy.Agent, role model.Proxy,
	certs []CertSource, pilotSAN []string) Watcher {
	return &watcher{
		agent:    agent,
		role:     role,
		config:   config,
		certs:    certs,
		pilotSAN: pilotSAN,
	}
}

const (
	// defaultMinDelay is the minimum amount of time between delivery of two successive events via updateFunc.
	defaultMinDelay = 10 * time.Second
)

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
	h := sha256.New()
	for _, cert := range w.certs {
		generateCertHash(h, cert.Directory, cert.Files)
	}

	w.agent.ScheduleConfigUpdate(h.Sum(nil))
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
			log.Infof("watchFileEvents: %s", ev.String())
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

			log.Info("watchFileEvents: notifying")
			notifyFn()
		case <-ctx.Done():
			log.Info("watchFileEvents has terminated")
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
		log.Warnf("failed to create a watcher for certificate files: %v", err)
		return
	}
	defer func() {
		if err := fw.Close(); err != nil {
			log.Warnf("closing watcher encounters an error %v", err)
		}
	}()

	// watch all directories
	for _, d := range certsDirs {
		if err := fw.Watch(d); err != nil {
			log.Warnf("watching %s encounters an error %v", d, err)
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
			// log.Warnf("failed to read file %q", filename)
			continue
		}
		if _, err := h.Write(bs); err != nil {
			log.Warna(err)
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
	config    meshconfig.ProxyConfig
	node      string
	extraArgs []string
	pilotSAN  []string
	opts      map[string]interface{}
	errChan   chan error
}

// NewProxy creates an instance of the proxy control commands
func NewProxy(config meshconfig.ProxyConfig, node string, logLevel string, pilotSAN []string) proxy.Proxy {
	// inject tracing flag for higher levels
	var args []string
	if logLevel != "" {
		args = append(args, "-l", logLevel)
	}

	return envoy{
		config:    config,
		node:      node,
		extraArgs: args,
		pilotSAN:  pilotSAN,
	}
}

func (proxy envoy) args(fname string, epoch int) []string {
	startupArgs := []string{"-c", fname,
		"--restart-epoch", fmt.Sprint(epoch),
		"--drain-time-s", fmt.Sprint(int(convertDuration(proxy.config.DrainDuration) / time.Second)),
		"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(proxy.config.ParentShutdownDuration) / time.Second)),
		"--service-cluster", proxy.config.ServiceCluster,
		"--service-node", proxy.node,
		"--max-obj-name-len", fmt.Sprint(MaxClusterNameLength), // TODO: use MeshConfig.StatNameLength instead
	}

	startupArgs = append(startupArgs, proxy.extraArgs...)

	if proxy.config.Concurrency > 0 {
		startupArgs = append(startupArgs, "--concurrency", fmt.Sprint(proxy.config.Concurrency))
	}

	if len(proxy.config.AvailabilityZone) > 0 {
		startupArgs = append(startupArgs, []string{"--service-zone", proxy.config.AvailabilityZone}...)
	}

	return startupArgs
}

func (proxy envoy) Run(config interface{}, epoch int, abort <-chan error) error {

	var fname string
	// Note: the cert checking still works, the generated file is updated if certs are changed.
	// We just don't save the generated file, but use a custom one instead. Pilot will keep
	// monitoring the certs and restart if the content of the certs changes.
	if len(proxy.config.CustomConfigFile) > 0 {
		// there is a custom configuration. Don't write our own config - but keep watching the certs.
		fname = proxy.config.CustomConfigFile
	} else {
		out, err := bootstrap.WriteBootstrap(&proxy.config, proxy.node, epoch, proxy.pilotSAN, proxy.opts)
		if err != nil {
			log.Errora("Failed to generate bootstrap config", err)
			os.Exit(1) // Prevent infinite loop attempting to write the file, let k8s/systemd report
			return err
		}
		fname = out
	}

	// spin up a new Envoy process
	args := proxy.args(fname, epoch)
	if len(proxy.config.CustomConfigFile) == 0 {
		args = append(args, "--v2-config-only")
	}
	log.Infof("Envoy command: %v", args)

	/* #nosec */
	cmd := exec.Command(proxy.config.BinaryPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	// Set if the caller is monitoring envoy, for example in tests or if envoy runs in same
	// container with the app.
	if proxy.errChan != nil {
		// Caller passed a channel, will wait itself for termination
		go func() {
			proxy.errChan <- cmd.Wait()
		}()
		return nil
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

func (proxy envoy) Cleanup(epoch int) {
	filePath := configFile(proxy.config.ConfigPath, epoch)
	if err := os.Remove(filePath); err != nil {
		log.Warnf("Failed to delete config file %s for %d, %v", filePath, epoch, err)
	}
}

func (proxy envoy) Panic(epoch interface{}) {
	log.Error("cannot start the proxy with the desired configuration")
	if epochInt, ok := epoch.(int); ok {
		// print the failed config file
		filePath := configFile(proxy.config.ConfigPath, epochInt)
		b, _ := ioutil.ReadFile(filePath)
		log.Errorf(string(b))
	}
	os.Exit(-1)
}
