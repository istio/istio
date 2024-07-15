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

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"

	"istio.io/istio/cni/pkg/nodeagent"
)

// newCNIClient is a unit test override variable for mocking.
var newCNIClient = buildClient

// An udsCore write entries to an UDS server with HTTP Post. Log messages will be encoded into a JSON array.
type CNIEventClient struct {
	client *http.Client
	url    string
}

func buildClient(address, path string) CNIEventClient {
	c := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", address)
			},
		},
		Timeout: 5 * time.Second,
	}
	eventC := CNIEventClient{
		client: c,
		url:    "http://unix" + path,
	}
	return eventC
}

func PushCNIEvent(cniClient CNIEventClient, event *skel.CmdArgs, prevResIps []*cniv1.IPConfig, podName, podNamespace string) error {
	if event == nil {
		return fmt.Errorf("unable to push CNI event, CmdArgs event was nil")
	}

	var ncconfigs []nodeagent.IPConfig
	for _, ipc := range prevResIps {
		ncconfigs = append(ncconfigs, nodeagent.IPConfig{Interface: ipc.Interface, Address: ipc.Address, Gateway: ipc.Gateway})
	}
	// Currently we only use the netns from the original CNI event
	addEvent := nodeagent.CNIPluginAddEvent{Netns: event.Netns, PodName: podName, PodNamespace: podNamespace, IPs: ncconfigs}
	eventData, err := json.Marshal(addEvent)
	if err != nil {
		return err
	}
	response, err := cniClient.client.Post(cniClient.url, "application/json", bytes.NewBuffer(eventData))
	if err != nil {
		return fmt.Errorf("failed to send event request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
		if err != nil {
			return fmt.Errorf("unable to push CNI event and failed to read body (status code %d): %v", response.StatusCode, err)
		}
		return fmt.Errorf("unable to push CNI event (status code %d): %v", response.StatusCode, string(body))
	}

	return nil
}
