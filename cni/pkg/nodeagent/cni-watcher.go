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

	"github.com/containernetworking/cni/pkg/skel"

	pconstants "istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/pluginlistener"
	"istio.io/istio/pkg/network"
)

// Just a composite of the CNI plugin add event struct + some extracted "args"
type CNIPluginAddEvent struct {
	CmdAddEvent  skel.CmdArgs
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

// startCNIPluginListener starts up a UDS server which receives events from the CNI chain plugin.
func (s *CniPluginServer) Start() error {
	if s.sockAddress == "" {
		return fmt.Errorf("no socket address provided")
	}
	log.Info("Start a listen server for CNI plugin events")
	unixListener, err := pluginlistener.NewListener(s.sockAddress)
	if err != nil {
		return fmt.Errorf("failed to create CNI listener: %v", err)
	}
	go func() {
		if err := s.cniListenServer.Serve(unixListener); network.IsUnexpectedListenerError(err) {
			log.Errorf("Error running CNI listener server: %v", err)
		}
	}()

	go func() {
		<-s.ctx.Done()
		if err := s.cniListenServer.Close(); err != nil {
			log.Errorf("CNI listen server terminated with error: %v", err)
		} else {
			log.Debug("CNI listen server terminated")
		}
	}()

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
		log.Errorf("Failed to read event report from cni plugin: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	msg, err := processAddEvent(data)
	if err != nil {
		log.Errorf("Failed to process CNI event payload: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.ReconcileCNIAddEvent(req.Context(), msg); err != nil {
		log.Errorf("Failed to handle add event: %v", err)
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

	log.Debugf("Deserialized CNI plugin event: %+v", msg)
	return msg, nil
}

func (s *CniPluginServer) ReconcileCNIAddEvent(ctx context.Context, addCmd CNIPluginAddEvent) error {
	log := log.WithLabels("cni-event", addCmd)

	// The CNI node plugin should have already checked the pod with this
	// exact same function before forwarding us the event, but we have to invoke the K8S client anyway,
	// so to be safe we check it again here to make sure we get the same result
	ambientPod, err := s.handlers.AmbientEnabled(addCmd.PodName, addCmd.PodNamespace)
	if err != nil {
		return err
	}

	if ambientPod != nil {
		log.Debugf("Pod: %s in ns: %s is enabled for ambient, adding to mesh. ", addCmd.PodName, addCmd.PodNamespace)

		var podIps []netip.Addr
		for _, configuredPodIPs := range addCmd.IPs {
			// net.ip is implicitly convertible to netip as slice
			ip, _ := netip.AddrFromSlice(configuredPodIPs.Address.IP)
			// We ignore the mask of the IPNet - it's fine if the IPNet defines
			// a block grant of addresses, we just need one for checking routes.
			podIps = append(podIps, ip)
		}
		// Note that we use the IP info from the CNI plugin here - the Pod struct as reported by K8S doesn't have this info
		// yet (because the K8S control plane doesn't), so it will be empty there.
		err := s.dataplane.AddPodToMesh(ctx, ambientPod, podIps, addCmd.CmdAddEvent.Netns)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("got an unexpected event for pod %s in namespace %s! This is a plugin bug", addCmd.PodName, addCmd.PodNamespace)
	}
	return nil
}
