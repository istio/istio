// Copyright 2016 Istio Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// An example implementation of Echo backend in go.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
)

var (
	port = flag.Int("port", 8080, "default http port")

	requests = 0
	data     = 0
	lds_n    = 0
)

// Test LDS update with command
//   envoy -c envoy_lds.conf --service-cluster cluster --service-node node
const listener1 = `{
  "listeners": [
     {
      "address": "tcp://0.0.0.0:9090",
      "name": "server-http",
      "bind_to_port": true,
      "filters": [
        {
          "type": "read",
          "name": "http_connection_manager",
          "config": {
            "codec_type": "auto",
	    "generate_request_id": true,
            "stat_prefix": "ingress_http",
            "route_config": {
              "virtual_hosts": [
                {
                  "name": "backend",
                  "domains": ["*"],
                  "routes": [
                    {
                      "timeout_ms": 0,
                      "prefix": "/",
                      "cluster": "service1",
                      "opaque_config": {
                        "mixer_control": "on",
                        "mixer_forward": "off"
                      }
                    }
                  ]
                }
              ]
            },
            "access_log": [
              {
                "path": "/dev/stdout"
              }
            ],
            "filters": [
              {
                "type": "decoder",
                "name": "mixer",
                "config": {
                  "mixer_attributes": {
                      "target.uid": "POD222",
                      "target.service": "foo.svc.cluster.local"
                  },
                  "random_string": "AAAAA",
                  "quota_name": "RequestCount",
                  "quota_amount": "1"
                }
              },
              {
                "type": "decoder",
                "name": "router",
                "config": {}
              }
            ]
          }
        }
      ]
    }
  ]
}
`

func lds_handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Generates a new config to cause Envoy to update the listener
	lds_n++
	w.Write([]byte(strings.Replace(listener1, "AAAAA", strconv.Itoa(lds_n), -1)))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("%v %v %v %v\n", r.Method, r.URL, r.Proto, r.RemoteAddr)
	for name, headers := range r.Header {
		for _, h := range headers {
			fmt.Printf("%v: %v\n", name, h)
		}
	}
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// echo back the Content-Type and Content-Length in the response
	for _, k := range []string{"Content-Type", "Content-Length"} {
		if v := r.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	w.Write(body)

	requests++
	data += len(body)
	fmt.Printf("Requests Requests: %v  Data: %v\n", requests, data)
}

func main() {
	flag.Parse()

	fmt.Printf("Listening on port %v\n", *port)

	http.HandleFunc("/", handler)
	http.HandleFunc("/v1/listeners/cluster/node", lds_handler)
	http.ListenAndServe(":"+strconv.Itoa(*port), nil)
}
