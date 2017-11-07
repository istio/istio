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
	"bytes"
	"context"
	"crypto/sha256"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	"github.com/howeyc/fsnotify"

	"istio.io/istio/pilot/proxy"
	"fmt"
)

type TestAgent struct {
	schedule func(interface{})
}

func (ta TestAgent) ScheduleConfigUpdate(config interface{}) {
	ta.schedule(config)
}

func (ta TestAgent) Run(ctx context.Context) {
	<-ctx.Done()
}

func TestRunReload(t *testing.T) {
	called := make(chan bool)
	agent := TestAgent{
		schedule: func(_ interface{}) {
			called <- true
		},
	}
	config := proxy.DefaultProxyConfig()
	node := proxy.Node{
		Type: proxy.Ingress,
		ID:   "random",
	}
	watcher := NewWatcher(config, agent, node, []CertSource{{Directory: "random"}}, nil)
	ctx, cancel := context.WithCancel(context.Background())

	// watcher starts agent and schedules a config update
	go watcher.Run(ctx)

	select {
	case <-called:
		// expected
		cancel()
	case <-time.After(time.Second):
		t.Errorf("The callback is not called within time limit " + time.Now().String())
		cancel()
	}
}

func TestWatchCerts_Multiple(t *testing.T) {
	called := 0
	callback := func() {
		called++
	}

	maxDelay := 500 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	wch := make(chan *fsnotify.FileEvent, 10)

	go watchFileEvents(ctx, wch, maxDelay, callback)

	// fire off multiple events
	wch <- &fsnotify.FileEvent{Name: "f1"}
	wch <- &fsnotify.FileEvent{Name: "f2"}
	wch <- &fsnotify.FileEvent{Name: "f3"}

	// sleep for less than maxDelay
	time.Sleep(maxDelay / 2)

	// Expect no events to be delivered within maxDelay.
	if called != 0 {
		t.Fatalf("Called %d times, want 0", called)
	}

	// wait for quiet period
	time.Sleep(maxDelay)

	// Expect exactly 1 event to be delivered.
	if called != 1 {
		t.Fatalf("Called %d times, want 1", called)
	}
	cancel()
}

func TestWatchCerts(t *testing.T) {
	name, err := ioutil.TempDir("testdata", "certs")
	if err != nil {
		t.Errorf("failed to create a temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(name); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	called := make(chan bool)
	callbackFunc := func() {
		called <- true
	}

	ctx, cancel := context.WithCancel(context.Background())

	go watchCerts(ctx, []string{name}, watchFileEvents, 50*time.Millisecond, callbackFunc)

	// sleep one second to make sure the watcher is set up before change is made
	time.Sleep(time.Second)

	// make a change to the watched dir
	if _, err := ioutil.TempFile(name, "test.file"); err != nil {
		t.Errorf("failed to create a temp file in testdata/certs: %v", err)
	}

	select {
	case <-called:
		// expected
		cancel()
	case <-time.After(time.Second):
		t.Errorf("The callback is not called within time limit " + time.Now().String())
		cancel()
	}

	// should terminate immediately
	go watchCerts(ctx, nil, watchFileEvents, 50*time.Millisecond, callbackFunc)
}

func TestGenerateCertHash(t *testing.T) {
	name, err := ioutil.TempDir("testdata", "certs")
	if err != nil {
		t.Errorf("failed to create a temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(name); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	h := sha256.New()
	authFiles := []string{proxy.CertChainFilename, proxy.KeyFilename, proxy.RootCertFilename}
	for _, file := range authFiles {
		content := []byte(file)
		if err := ioutil.WriteFile(path.Join(name, file), content, 0644); err != nil {
			t.Errorf("failed to write file %s (error %v)", file, err)
		}
		if _, err := h.Write(content); err != nil {
			t.Errorf("failed to write hash (error %v)", err)
		}
	}
	expectedHash := h.Sum(nil)

	h2 := sha256.New()
	generateCertHash(h2, name, append(authFiles, "missing-file"))
	actualHash := h2.Sum(nil)
	if !bytes.Equal(actualHash, expectedHash) {
		t.Errorf("Actual hash value (%v) is different than the expected hash value (%v)", actualHash, expectedHash)
	}

	generateCertHash(h2, "", nil)
	emptyHash := h2.Sum(nil)
	if !bytes.Equal(emptyHash, expectedHash) {
		t.Error("hash should not be affected by empty directory")
	}
}

func TestEnvoyArgs(t *testing.T) {
	config := proxy.DefaultProxyConfig()
	config.ServiceCluster = "my-cluster"
	config.AvailabilityZone = "my-zone"

	test := envoy{config: config, node: "my-node"}
	testProxy := NewProxy(config, "my-node")
	if !reflect.DeepEqual(testProxy, test) {
		t.Errorf("unexpected struct got\n%v\nwant\n%v", testProxy, test)
	}

	got := test.args("test.json", 5)
	want := []string{
		"-c", "test.json",
		"--restart-epoch", "5",
		"--drain-time-s", "2",
		"--parent-shutdown-time-s", "3",
		"--service-cluster", "my-cluster",
		"--service-node", "my-node",
		"--max-obj-name-len", fmt.Sprint(MaxClusterNameLength), // TODO: use MeshConfig.StatNameLength instead
		"--service-zone", "my-zone",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("envoyArgs() => got %v, want %v", got, want)
	}
}

func TestEnvoyRun(t *testing.T) {
	config := proxy.DefaultProxyConfig()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	config.BinaryPath = path.Join(dir, "envoy")
	config.ConfigPath = "tmp"

	envoyConfig := buildConfig(config, nil)
	proxy := envoy{config: config, node: "my-node", extraArgs: []string{"--mode", "validate"}}
	abortCh := make(chan error, 1)

	if err = proxy.Run(nil, 0, abortCh); err == nil {
		t.Error("expected error on nil config")
	}

	if err = proxy.Run(envoyConfig, 0, abortCh); err != nil {
		t.Error(err)
	}

	proxy.Cleanup(0)

	badConfig := config
	badConfig.ConfigPath = ""
	proxy.config = badConfig

	if err = proxy.Run(envoyConfig, 0, abortCh); err == nil {
		t.Errorf("expected error on bad config path")
	}
}
