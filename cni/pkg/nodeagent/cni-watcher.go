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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	corev1 "k8s.io/api/core/v1"

	pconstants "istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/pluginlistener"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/sleep"
)

// Just a composite of the CNI plugin add event struct + some extracted "args"
type CNIPluginAddEvent struct {
	Netns        string
	PodName      string
	PodNamespace string
	IPs          []IPConfig
}

// IPConfig contains an interface/gateway/address combo defined for a newly-started pod by CNI.
// This is "from the horse's mouth" so to speak and will be populated before Kube is informed of the
// pod IP.
type IPConfig struct {
	Interface *int
	Address   net.IPNet
	Gateway   net.IP
}

type CniPluginServer struct {
	cniListenServer       *http.Server
	cniListenServerCancel context.CancelFunc
	handlers              K8sHandlers
	dataplane             MeshDataplane

	sockAddress string
	ctx         context.Context
}

func startCniPluginServer(ctx context.Context, pluginSocket string,
	handlers K8sHandlers,
	dataplane MeshDataplane,
) *CniPluginServer {
	ctx, cancel := context.WithCancel(ctx)
	mux := http.NewServeMux()
	s := &CniPluginServer{
		handlers:  handlers,
		dataplane: dataplane,
		cniListenServer: &http.Server{
			Handler: mux,
		},
		cniListenServerCancel: cancel,
		sockAddress:           pluginSocket,
		ctx:                   ctx,
	}

	mux.HandleFunc(pconstants.CNIAddEventPath, s.handleAddEvent)
	return s
}

func (s *CniPluginServer) Stop() {
	s.cniListenServerCancel()
}

// Start starts up a UDS server which receives events from the CNI chain plugin.
func (s *CniPluginServer) Start() error {
	if s.sockAddress == "" {
		return fmt.Errorf("no socket address provided")
	}
	log.Infof("starting listener for CNI plugin events at %v", s.sockAddress)
	unixListener, err := pluginlistener.NewListener(s.sockAddress)
	if err != nil {
		return fmt.Errorf("failed to create CNI listener: %v", err)
	}
	go func() {
		err := s.cniListenServer.Serve(unixListener)

		select {
		case <-s.ctx.Done():
			// ctx done, we should silently go away
			return
		default:
			// If the cniListener exits, at least we should record an error log
			log.Errorf("CNI listener server exiting unexpectedly: %v", err)
		}
	}()

	context.AfterFunc(s.ctx, func() {
		if err := s.cniListenServer.Close(); err != nil {
			log.Errorf("CNI listen server terminated with error: %v", err)
		} else {
			log.Debug("CNI listen server terminated")
		}
	})
	return nil
}

func (s *CniPluginServer) handleAddEvent(w http.ResponseWriter, req *http.Request) {
	if req.Body == nil {
		log.Error("empty request body")
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}
	defer req.Body.Close()
	data, err := io.ReadAll(req.Body)
	if err != nil {
		log.Errorf("failed to read event report from cni plugin: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	msg, err := processAddEvent(data)
	if err != nil {
		log.Errorf("failed to process CNI event payload: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.ReconcileCNIAddEvent(req.Context(), msg); err != nil {
		log.WithLabels("ns", msg.PodNamespace, "name", msg.PodName).Errorf("failed to handle add event: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func processAddEvent(body []byte) (CNIPluginAddEvent, error) {
	var msg CNIPluginAddEvent
	err := json.Unmarshal(body, &msg)
	if err != nil {
		log.Errorf("Failed to unmarshal CNI plugin event: %v", err)
		return msg, err
	}

	log.Infof("Deserialized CNI plugin event: %+v", msg)
	return msg, nil
}

func (s *CniPluginServer) ReconcileCNIAddEvent(ctx context.Context, addCmd CNIPluginAddEvent) error {
	log := log.WithLabels("cni-event", addCmd)

	log.Infof("netns: %s", addCmd.Netns)

	// The CNI node plugin should have already checked the pod against the k8s API before forwarding us the event,
	// but we have to invoke the K8S client anyway, so to be safe we check it again here to make sure we get the same result.
	ambientPod, err := s.getPodWithRetry(log, addCmd.PodName, addCmd.PodNamespace)
	if err != nil {
		return err
	}
	log.Infof("Pod: %s in ns: %s is enabled for ambient, adding to mesh.", addCmd.PodName, addCmd.PodNamespace)

	var podIps []netip.Addr
	for _, configuredPodIPs := range addCmd.IPs {
		// net.ip is implicitly convertible to netip as slice
		ip, _ := netip.AddrFromSlice(configuredPodIPs.Address.IP)
		// We ignore the mask of the IPNet - it's fine if the IPNet defines
		// a block grant of addresses, we just need one for checking routes.
		podIps = append(podIps, ip.Unmap())
	}
	// Note that we use the IP info from the CNI plugin here - the Pod struct as reported by K8S doesn't have this info
	// yet (because the K8S control plane doesn't), so it will be empty there.
	err = s.dataplane.AddPodToMesh(ctx, ambientPod, podIps, addCmd.Netns)
	if err != nil {
		return err
	}

	return nil
}

func (s *CniPluginServer) getPodWithRetry(log *istiolog.Scope, name, namespace string) (*corev1.Pod, error) {
	log.Infof("Checking if pod %s/%s is enabled for ambient", namespace, name)
	const maxStaleRetries = 10
	const msInterval = 10
	retries := 0
	var ambientPod *corev1.Pod
	var err error

	// The plugin already consulted the k8s API - but on this end handler caches may be stale, so retry a few times if we get no pod.
	// if err is returned, we couldn't find the pod
	// if nil is returned, we found it but ambient is not enabled
	for ambientPod, err = s.handlers.GetPodIfAmbientEnabled(name, namespace); (err != nil) && (retries < maxStaleRetries); retries++ {
		log.Warnf("got an event for pod %s in namespace %s not found in current pod cache, retry %d of %d",
			name, namespace, retries, maxStaleRetries)
		if !sleep.UntilContext(s.ctx, time.Duration(msInterval)*time.Millisecond) {
			return nil, fmt.Errorf("aborted")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s/%s: %v", namespace, name, err)
	}

	// This shouldn't happen - the CNI plugin should only invoke us when a pod starts up that already meets
	// ambient eligibility requirements.
	if ambientPod == nil {
		return nil, fmt.Errorf("pod %s/%s is unexpectedly not eligible for ambient enrollment", namespace, name)
	}
	return ambientPod, nil
}
