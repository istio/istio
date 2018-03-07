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
	"reflect"
	"strings"
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// we want to save the original state of tt.a for printing if we fail the test, so we'll merge into a new gateway struct.
			actual := &networking.Gateway{}
			MergeGateways(actual, tt.a) // nolint: errcheck
			err := MergeGateways(actual, tt.b)
			if err != tt.expectedErrPrefix && !strings.HasPrefix(err.Error(), tt.expectedErrPrefix.Error()) {
				t.Fatalf("%s: got err %v, wanted %v", tt.name, err, tt.expectedErrPrefix)
			}
			if err == nil {
				if !reflect.DeepEqual(actual, tt.expectedOut) {
					t.Fatalf("%s: got %v, wanted %v", tt.name, actual, tt.expectedOut)
				}
			}
		})
	}
}

func TestPortsEqualAndDistinct(t *testing.T) {
	tests := []struct {
		name             string
		b                *networking.Port
		a                *networking.Port
		expectedEqual    bool
		expectedDistinct bool
	}{
		{"empty", &networking.Port{}, &networking.Port{}, true, false},
		{"happy",
			&networking.Port{Number: 1, Name: "Bill", Protocol: "HTTP"},
			&networking.Port{Number: 1, Name: "Bill", Protocol: "HTTP"},
			true, false},
		{"same numbers but different names (case sensitive)",
			&networking.Port{Number: 1, Name: "Bill", Protocol: "HTTP"},
			&networking.Port{Number: 1, Name: "bill", Protocol: "HTTP"},
			false, false},
		{"case insensitive",
			&networking.Port{Number: 1, Name: "potato", Protocol: "GRPC"},
			&networking.Port{Number: 1, Name: "potato", Protocol: "grpc"},
			true, false},
		{"different numbers but same names",
			&networking.Port{Number: 1, Name: "potato", Protocol: "tcp"},
			&networking.Port{Number: 2, Name: "potato", Protocol: "tcp"},
			false, false},
		{"different protocols but same names",
			&networking.Port{Number: 1, Name: "potato", Protocol: "http2"},
			&networking.Port{Number: 1, Name: "potato", Protocol: "http"},
			false, false},
		{"different numbers and different names",
			&networking.Port{Number: 1, Name: "potato", Protocol: "tcp"},
			&networking.Port{Number: 2, Name: "banana", Protocol: "tcp"},
			false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualEquality := portsEqual(tt.a, tt.b)
			if actualEquality != tt.expectedEqual {
				t.Fatalf("%s: [%v] =?= [%v] got %t, wanted %t", tt.name, tt.a, tt.b, actualEquality, tt.expectedEqual)
			}
			actualDistinct := portsAreDistinct(tt.a, tt.b)
			if actualDistinct != tt.expectedDistinct {
				t.Fatalf("%s: [%v] =?= [%v] got %t, wanted %t", tt.name, tt.a, tt.b, actualDistinct, tt.expectedDistinct)
			}
		})
	}
}

func TestTlsEqual(t *testing.T) {
	tests := []struct {
		name  string
		b     *networking.Server_TLSOptions
		a     *networking.Server_TLSOptions
		equal bool
	}{
		{"empty", &networking.Server_TLSOptions{}, &networking.Server_TLSOptions{}, true},
		{"happy",
			&networking.Server_TLSOptions{HttpsRedirect: true, Mode: networking.Server_TLSOptions_SIMPLE, ServerCertificate: "server.pem", PrivateKey: "key.pem"},
			&networking.Server_TLSOptions{HttpsRedirect: true, Mode: networking.Server_TLSOptions_SIMPLE, ServerCertificate: "server.pem", PrivateKey: "key.pem"},
			true},
		{"different",
			&networking.Server_TLSOptions{HttpsRedirect: true, Mode: networking.Server_TLSOptions_SIMPLE, ServerCertificate: "server.pem", PrivateKey: "key.pem"},
			&networking.Server_TLSOptions{HttpsRedirect: false},
			false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tlsEqual(tt.a, tt.b)
			if actual != tt.equal {
				t.Fatalf("tlsEqual(%v, %v) = %t, wanted %v", tt.a, tt.b, actual, tt.equal)
			}
		})
	}
}
