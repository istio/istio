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

package nodeagent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/pkg/monitoring/monitortest"
)

// To run this locally, do:
//   ZTUNNEL_PATH=/path/to/ztunnel go test -v -exec "sudo -E" .

func TestServerAndZtunIntegration(t *testing.T) {
	mt := monitortest.New(t)
	// only run this if we are root and can find ztunnel
	if os.Geteuid() != 0 {
		t.Skip("skipping test; must be root to run")
	}

	// find ztunnel
	ztunnel := os.Getenv("ZTUNNEL_PATH")
	if ztunnel == "" {
		ztunnel = "ztunnel"
	}

	ztunnel, err := exec.LookPath(ztunnel)
	if err != nil {
		t.Skip("skipping test; ztunnel binary is required but not present")
	}

	ztunnelKeepAliveCheckInterval = time.Second / 10
	setupLogging()
	addr := "/tmp/foo"
	podNsMap := newPodNetnsCache(OpenNetnsInRoot("/"))
	ztunnelServer, err := newZtunnelServer(addr, podNsMap)
	if err != nil {
		panic(err)
	}
	iptablesConfigurator := iptables.NewIptablesConfigurator(nil, realDependencies(), iptables.RealNlDeps())
	netServer := newNetServer(ztunnelServer, podNsMap, iptablesConfigurator, NewPodNetnsProcFinder(os.DirFS("/proc")))

	defer os.Remove(addr)
	ctx, cancelTimeout := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelTimeout()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	netServer.Start(ctx)
	defer netServer.Stop()

	cmd := exec.CommandContext(ctx, ztunnel, "proxy")
	cmd.Env = append(cmd.Env, "RUST_LOG=debug", "INPOD_ENABLED=true",
		"INPOD_UDS=/tmp/foo", "FAKE_CA=true", "XDS_ADDRESS=", "LOCAL_XDS_PATH=./testdata/localhost.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		panic(err)
	}
	var signalOnce sync.Once
	signal := func() {
		signalOnce.Do(func() {
			cmd.Process.Signal(os.Interrupt)
		})
	}
	defer signal()

	// wait until ztunnel is ready
	err = waitReady(ctx)
	if err != nil {
		panic(err)
	}

	ns, err := NewNS()
	if err != nil {
		panic(err)
	}
	defer ns.Close()

	fdPath := fmt.Sprintf("/proc/self/fd/%d", ns.Fd())
	kubeletIP := netip.MustParseAddr("99.9.9.9")
	kubeletIPs := []netip.Addr{kubeletIP}
	fakePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			UID:       "bar",
			Namespace: "baz",
		},
	}
	err = netServer.AddPodToMesh(ctx, fakePod, kubeletIPs, fdPath)
	if err != nil {
		panic(err)
	}

	log.Info("added pod to mesh, removing now")

	err = netServer.RemovePodFromMesh(ctx, fakePod)
	if err != nil {
		t.Fatalf("error removing pod %v", err)
	}

	// TODO: make sure the ztunnel has started its proxy in the netns
	// TODO: kill ztunnel and make sure it is connection is removed
	signal()
	cmd.Process.Wait()
	time.Sleep(time.Second / 2) // wait a bit for polling to detect ztunnel exit
	// check connection count it should be zero as the ztunnel process is gone
	mt.Assert(ztunnelConnected.Name(), nil, monitortest.Exactly(0))
}

func waitReady(ctx context.Context) error {
	client := &http.Client{}

	req, err := http.NewRequest("GET", "http://localhost:15021/healthz/ready", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	for {
		// Create a derived context with a deadline for the HTTP request
		req = req.WithContext(ctx)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("Error making HTTP request, retrying...", err)
		} else {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				fmt.Println("Error reading HTTP response, retrying...", err)
			} else if strings.TrimSpace(string(body)) == "ready" {
				fmt.Println("Endpoint is ready!")
				return nil
			} else {
				fmt.Println("Endpoint is not ready, retrying...")
			}
		}

		// Wait for a second or until the context is done
		select {
		case <-time.After(1 * time.Second):
		case <-ctx.Done():
			// Context has been cancelled, return with context error
			return ctx.Err()
		}
	}
}

// NewNS creates a new network namespace
func NewNS() (ns.NetNS, error) {
	// Lock the OS Thread so we don't accidentally switch namespaces
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	curNetNS, err := ns.GetCurrentNS()
	if err != nil {
		return nil, fmt.Errorf("failed to get current netns: %w", err)
	}

	err = syscall.Unshare(syscall.CLONE_NEWNET)
	if err != nil {
		return nil, fmt.Errorf("failed to unshare namespaces: %w", err)
	}

	netNS, err := ns.GetCurrentNS()
	if err != nil {
		return nil, fmt.Errorf("failed to get new netns: %w", err)
	}
	// Set the current netns back to the original
	err = curNetNS.Set()
	if err != nil {
		return nil, fmt.Errorf("failed to get new netns: %w", err)
	}

	return netNS, nil
}
