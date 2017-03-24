// Copyright 2017 Istio Authors. All Rights Reserved.
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

package test

import (
	"fmt"
	"os"
	"text/template"
)

const (
	// These ports should match with used envoy.conf
	// Default is using one in this folder.
	ServerProxyPort = 29090
	ClientProxyPort = 27070
	MixerPort       = 29091
	BackendPort     = 28080
	AdminPort       = 29001
)

type ConfParam struct {
	ClientPort   int
	ServerPort   int
	AdminPort    int
	MixerServer  string
	Backend      string
	ClientConfig string
	ServerConfig string
}

// A basic config
const basicConfig = `
                  "mixer_attributes": {
                      "target.uid": "POD222",
                      "target.namespace": "XYZ222"
                  }
`

// A config with quota
const quotaConfig = `
                  "quota_name": "RequestCount",
                  "quota_amount": "5"
`

// A config with check cache keys
const checkCacheConfig = `
                  "check_cache_keys": [
                      "request.host",
                      "request.path",
                      "origin.user"
                  ]
`

// The default client proxy mixer config
const defaultClientMixerConfig = `
                   "forward_attributes": {
                      "source.uid": "POD11",
                      "source.namespace": "XYZ11"
                   }
`

// The envoy config template
const envoyConfTempl = `
{
  "listeners": [
    {
      "address": "tcp://0.0.0.0:{{.ServerPort}}",
      "bind_to_port": true,
      "filters": [
        {
          "type": "read",
          "name": "http_connection_manager",
          "config": {
            "codec_type": "auto",
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
                  "mixer_server": "{{.MixerServer}}",
{{.ServerConfig}}
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
    },
    {
      "address": "tcp://0.0.0.0:{{.ClientPort}}",
      "bind_to_port": true,
      "filters": [
        {
          "type": "read",
          "name": "http_connection_manager",
          "config": {
            "codec_type": "auto",
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
                      "cluster": "service2"
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
                   "mixer_server": "{{.MixerServer}}",
{{.ClientConfig}}
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
  ],
  "admin": {
    "access_log_path": "/dev/stdout",
    "address": "tcp://0.0.0.0:{{.AdminPort}}"
  },
  "cluster_manager": {
    "clusters": [
      {
        "name": "service1",
        "connect_timeout_ms": 5000,
        "type": "strict_dns",
        "lb_type": "round_robin",
        "hosts": [
          {
            "url": "tcp://{{.Backend}}"
          }
        ]
      },
      {
        "name": "service2",
        "connect_timeout_ms": 5000,
        "type": "strict_dns",
        "lb_type": "round_robin",
        "hosts": [
          {
            "url": "tcp://localhost:{{.ServerPort}}"
          }
        ]
      }
    ]
  }
}
`

func (c *ConfParam) write(path string) error {
	tmpl, err := template.New("test").Parse(envoyConfTempl)
	if err != nil {
		return fmt.Errorf("Failed to parse config template: %v", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Failed to create file %v: %v", path, err)
	}
	defer f.Close()
	return tmpl.Execute(f, *c)
}

func getConf() ConfParam {
	return ConfParam{
		ClientPort:   ClientProxyPort,
		ServerPort:   ServerProxyPort,
		AdminPort:    AdminPort,
		MixerServer:  fmt.Sprintf("localhost:%d", MixerPort),
		Backend:      fmt.Sprintf("localhost:%d", BackendPort),
		ClientConfig: defaultClientMixerConfig,
	}
}

func CreateEnvoyConf(path string, conf string) error {
	c := getConf()
	c.ServerConfig = conf
	return c.write(path)
}
