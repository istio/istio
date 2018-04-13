// Copyright 2018 Istio Authors.
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

package model

import (
	"errors"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
)

var (
	tlsOne = &networking.Server_TLSOptions{
		HttpsRedirect: false,
	}
	tlsTwo = &networking.Server_TLSOptions{
		HttpsRedirect:     true,
		Mode:              networking.Server_TLSOptions_SIMPLE,
		ServerCertificate: "server.pem",
		PrivateKey:        "key.pem",
	}
	port80 = &networking.Port{
		Number:   80,
		Name:     "http-foo",
		Protocol: "HTTP",
	}
	port80DifferentName = &networking.Port{
		Number:   80,
		Name:     "http-FOO",
		Protocol: "HTTP",
	}
	port443 = &networking.Port{
		Number:   443,
		Name:     "https-foo",
		Protocol: "HTTPS",
	}
)

func TestMergeGateways(t *testing.T) {
	tests := []struct {
		name              string
		b                 *networking.Gateway
		a                 *networking.Gateway
		expectedOut       *networking.Gateway
		expectedErrPrefix error
	}{
		{"idempotent",
			&networking.Gateway{Servers: []*networking.Server{}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port443,
					Tls:   tlsOne,
					Hosts: []string{"example.com"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port443,
					Tls:   tlsOne,
					Hosts: []string{"example.com"},
				},
			}},
			nil,
		},
		{"different ports",
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"example.com"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port443,
					Tls:   tlsTwo,
					Hosts: []string{"example.com"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"example.com"},
				}, {
					Port:  port443,
					Tls:   tlsTwo,
					Hosts: []string{"example.com"},
				}}},
			nil,
		},
		{"same ports different domains (Multiple Hosts)",
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"foo.com"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"bar.com"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"bar.com", "foo.com"},
				},
			}},
			nil,
		},
		{"different domains, different ports",
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"foo.com"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port443,
					Tls:   tlsTwo,
					Hosts: []string{"bar.com"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"foo.com"},
				}, {
					Port:  port443,
					Tls:   tlsTwo,
					Hosts: []string{"bar.com"},
				}}},
			nil,
		},
		{"conflicting port names",
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"foo.com"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80DifferentName,
					Tls:   tlsOne,
					Hosts: []string{"bar.com"},
				},
			}},
			nil,
			errors.New(`unable to merge gateways: conflicting ports`),
		},
		{"wildcard hosts",
			&networking.Gateway{},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"*"},
				},
			}},
			&networking.Gateway{Servers: []*networking.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"*"},
				},
			}},
			nil,
		},
	}

	t.Fatalf("not implemented")
	for _, tt := range tests {
		_ = tt
	}
}
