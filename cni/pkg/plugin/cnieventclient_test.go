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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"

	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/nodeagent"
	"istio.io/istio/pkg/test/util/assert"
)

func fakeCNIEventClient(address string) CNIEventClient {
	c := http.DefaultClient

	eventC := CNIEventClient{
		client: c,
		url:    address + constants.CNIAddEventPath,
	}
	return eventC
}

var fakeCmdArgs = &skel.CmdArgs{
	Netns:       "someNetNS",
	IfName:      "ethBro",
	ContainerID: "bbb-eee-www",
}

var (
	fakeIP                 = net.ParseIP("99.9.9.9")
	fakeGW                 = net.ParseIP("88.9.9.9")
	fakeIDX                = 0
	fakePrevResultIPConfig = cniv1.IPConfig{
		Interface: &fakeIDX,
		Address: net.IPNet{
			IP: fakeIP,
		},
		Gateway: fakeGW,
	}
)

func TestPushCNIAddEventSucceed(t *testing.T) {
	// Fake out a test HTTP server and use that instead of a real HTTP server over gRPC to validate  req/resp flows
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(http.StatusOK)
		res.Write([]byte("server happy"))
	}))
	defer func() { testServer.Close() }()

	cniC := fakeCNIEventClient(testServer.URL)

	err := PushCNIEvent(cniC, fakeCmdArgs, []*cniv1.IPConfig{&fakePrevResultIPConfig}, "testpod", "testns")

	assert.NoError(t, err)
}

func TestPushCNIAddEventNotOK(t *testing.T) {
	// Fake out a test HTTP server and use that instead of a real HTTP server over gRPC to validate  req/resp flows
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(http.StatusInternalServerError)
		res.Write([]byte("server pooped itself"))
	}))
	defer func() { testServer.Close() }()

	cniC := fakeCNIEventClient(testServer.URL)

	err := PushCNIEvent(cniC, fakeCmdArgs, []*cniv1.IPConfig{&fakePrevResultIPConfig}, "testpod", "testns")

	assert.Error(t, err)
	assert.Equal(t, strings.Contains(err.Error(), fmt.Sprintf("unable to push CNI event (status code %d)", http.StatusInternalServerError)), true)
}

func TestPushCNIAddEventGoodPayload(t *testing.T) {
	testPod := "testpod"
	testNS := "testns"
	// Fake out a test HTTP server and use that instead of a real HTTP server over gRPC to validate  req/resp flows
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		defer req.Body.Close()
		data, err := io.ReadAll(req.Body)
		assert.NoError(t, err)

		var msg nodeagent.CNIPluginAddEvent
		err = json.Unmarshal(data, &msg)
		assert.NoError(t, err)
		assert.Equal(t, msg.PodName, testPod)
		assert.Equal(t, msg.PodNamespace, testNS)
		assert.Equal(t, msg.Netns, fakeCmdArgs.Netns)
		res.WriteHeader(http.StatusOK)
		res.Write([]byte("server happy"))
	}))
	defer func() { testServer.Close() }()

	cniC := fakeCNIEventClient(testServer.URL)

	err := PushCNIEvent(cniC, fakeCmdArgs, []*cniv1.IPConfig{&fakePrevResultIPConfig}, testPod, testNS)

	assert.NoError(t, err)
}

func TestPushCNIAddEventBadPayload(t *testing.T) {
	testPod := "testpod"
	testNS := "testns"
	// Fake out a test HTTP server and use that instead of a real HTTP server over gRPC to validate  req/resp flows
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		assert.Equal(t, true, false) // if we get here, it's an autofail
	}))
	defer func() { testServer.Close() }()

	cniC := fakeCNIEventClient(testServer.URL)

	err := PushCNIEvent(cniC, nil, nil, testPod, testNS)

	assert.Error(t, err)
	assert.Equal(t, strings.Contains(err.Error(), "CmdArgs event was nil"), true)
}
