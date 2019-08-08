// Copyright 2019 Istio Authors
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

package util

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"istio.io/istio/pilot/pkg/model"
)

type mockServer struct {
	listeners string
	server    *httptest.Server
}

func (ms *mockServer) serve(adminPort uint16) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/listeners", http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		values, ok := req.URL.Query()["format"]
		if !ok || len(values) != 1 || values[0] != "json" {
			_, _ = rw.Write([]byte("invalid query param, only support: format=json"))
		}
		_, _ = rw.Write([]byte(ms.listeners))
	}))
	ms.server = httptest.NewUnstartedServer(mux)

	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", adminPort))
	if err != nil {
		return err
	}
	ms.server.Listener = l
	ms.server.Start()
	return nil
}

func (ms *mockServer) setListeners(listeners string) {
	ms.listeners = listeners
}

func (ms *mockServer) shutdown() {
	ms.server.CloseClientConnections()
}

func TestGetInboundListeningPorts(t *testing.T) {
	ms := mockServer{}
	if err := ms.serve(0); err != nil {
		t.Fatalf("failed to start mock server: %s", err)
	}
	adminPort := uint16(ms.server.Listener.Addr().(*net.TCPAddr).Port)
	defer func() {
		ms.shutdown()
	}()

	testCases := []struct {
		name       string
		nodeType   model.NodeType
		ipPrefixes []string
		listeners  string
		wantPorts  map[uint16]bool
	}{
		{
			name:       "SidecarProxy",
			nodeType:   model.SidecarProxy,
			ipPrefixes: []string{"10.0.0.1", "10.0.0.2", "1004:"},
			listeners: `
{
 "listener_statuses": [
  {
   "name": "10.0.0.1_1001",
   "local_address": {
    "socket_address": {
     "address": "10.0.0.1",
     "port_value": 1001
    }
   }
  },
  {
   "name": "10.0.0.1_1002",
   "local_address": {
    "socket_address": {
     "address": "10.0.0.1",
     "port_value": 1002
    }
   }
  },
  {
   "name": "10.0.0.2_2001",
   "local_address": {
    "socket_address": {
     "address": "10.0.0.2",
     "port_value": 2001
    }
   }
  },
  {
   "name": "10.0.0.2_2002",
   "local_address": {
    "socket_address": {
     "address": "10.0.0.2",
     "port_value": 2002
    }
   }
  },
  {
   "name": "10.0.0.3_3001",
   "local_address": {
    "socket_address": {
     "address": "10.0.0.3",
     "port_value": 3001
    }
   }
  },
  {
   "name": "[1004::ffff]_4001",
   "local_address": {
    "socket_address": {
     "address": "[1004::ffff]",
     "port_value": 4001
    }
   }
  }
 ]
}`,
			wantPorts: map[uint16]bool{
				1001: true,
				1002: true,
				2001: true,
				2002: true,
				4001: true,
			},
		},
		{
			name:       "Router",
			nodeType:   model.Router,
			ipPrefixes: []string{"10.0.0.1", "10.0.0.2"},
			listeners: `
{
 "listener_statuses": [
  {
   "name": "10.0.0.1_1001",
   "local_address": {
    "socket_address": {
     "address": "10.0.0.1",
     "port_value": 1001
    }
   }
  },
  {
   "name": "10.0.0.2_2001",
   "local_address": {
    "socket_address": {
     "address": "10.0.0.2",
     "port_value": 2001
    }
   }
  },
  {
   "name": "0.0.0.0_80",
   "local_address": {
    "socket_address": {
     "address": "0.0.0.0",
     "port_value": 80
    }
   }
  },
  {
   "name": "0.0.0.0_90",
   "local_address": {
    "socket_address": {
     "address": "0.0.0.0",
     "port_value": 90
    }
   }
  }
 ]
}`,
			wantPorts: map[uint16]bool{
				80: true,
				90: true,
			},
		},
	}

	for _, tc := range testCases {
		ipPrefixes = tc.ipPrefixes
		ms.setListeners(tc.listeners)
		got, _, err := GetInboundListeningPorts("127.0.0.1", adminPort, tc.nodeType)
		if err != nil {
			t.Fatalf("%s: %s", tc.name, err)
		}
		if !reflect.DeepEqual(got, tc.wantPorts) {
			t.Errorf("%s: got %v but want %v", tc.name, got, tc.wantPorts)
		}
	}
}
