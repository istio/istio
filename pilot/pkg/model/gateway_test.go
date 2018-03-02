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

	routing "istio.io/api/networking/v1alpha3"
)

var (
	tlsOne = &routing.Server_TLSOptions{
		HttpsRedirect: false,
	}
	tlsTwo = &routing.Server_TLSOptions{
		HttpsRedirect:     true,
		Mode:              routing.Server_TLSOptions_SIMPLE,
		ServerCertificate: "server.pem",
		PrivateKey:        "key.pem",
	}
	port80 = &routing.Port{
		Number:   80,
		Name:     "http-foo",
		Protocol: "HTTP",
	}
	port80DifferentName = &routing.Port{
		Number:   80,
		Name:     "http-FOO",
		Protocol: "HTTP",
	}
	port443 = &routing.Port{
		Number:   443,
		Name:     "https-foo",
		Protocol: "HTTPS",
	}
)

func TestMergeGateways(t *testing.T) {
	tests := []struct {
		name              string
		b                 *routing.Gateway
		a                 *routing.Gateway
		expectedOut       *routing.Gateway
		expectedErrPrefix error
	}{
		{"idempotent",
			&routing.Gateway{Servers: []*routing.Server{}},
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port443,
					Tls:   tlsOne,
					Hosts: []string{"example.com"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port443,
					Tls:   tlsOne,
					Hosts: []string{"example.com"},
				},
			}},
			nil,
		},
		{"different ports",
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"example.com"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port443,
					Tls:   tlsTwo,
					Hosts: []string{"example.com"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
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
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"foo.com"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"bar.com"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"bar.com", "foo.com"},
				},
			}},
			nil,
		},
		{"different domains, different ports",
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"foo.com"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port443,
					Tls:   tlsTwo,
					Hosts: []string{"bar.com"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
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
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"foo.com"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
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
			&routing.Gateway{},
			&routing.Gateway{Servers: []*routing.Server{
				{
					Port:  port80,
					Tls:   tlsOne,
					Hosts: []string{"*"},
				},
			}},
			&routing.Gateway{Servers: []*routing.Server{
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
			actual := &routing.Gateway{}
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
		b                *routing.Port
		a                *routing.Port
		expectedEqual    bool
		expectedDistinct bool
	}{
		{"empty", &routing.Port{}, &routing.Port{}, true, false},
		{"happy",
			&routing.Port{Number: 1, Name: "Bill", Protocol: "HTTP"},
			&routing.Port{Number: 1, Name: "Bill", Protocol: "HTTP"},
			true, false},
		{"same numbers but different names (case sensitive)",
			&routing.Port{Number: 1, Name: "Bill", Protocol: "HTTP"},
			&routing.Port{Number: 1, Name: "bill", Protocol: "HTTP"},
			false, false},
		{"case insensitive",
			&routing.Port{Number: 1, Name: "potato", Protocol: "GRPC"},
			&routing.Port{Number: 1, Name: "potato", Protocol: "grpc"},
			true, false},
		{"different numbers but same names",
			&routing.Port{Number: 1, Name: "potato", Protocol: "tcp"},
			&routing.Port{Number: 2, Name: "potato", Protocol: "tcp"},
			false, false},
		{"different protocols but same names",
			&routing.Port{Number: 1, Name: "potato", Protocol: "http2"},
			&routing.Port{Number: 1, Name: "potato", Protocol: "http"},
			false, false},
		{"different numbers and different names",
			&routing.Port{Number: 1, Name: "potato", Protocol: "tcp"},
			&routing.Port{Number: 2, Name: "banana", Protocol: "tcp"},
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
		b     *routing.Server_TLSOptions
		a     *routing.Server_TLSOptions
		equal bool
	}{
		{"empty", &routing.Server_TLSOptions{}, &routing.Server_TLSOptions{}, true},
		{"happy",
			&routing.Server_TLSOptions{HttpsRedirect: true, Mode: routing.Server_TLSOptions_SIMPLE, ServerCertificate: "server.pem", PrivateKey: "key.pem"},
			&routing.Server_TLSOptions{HttpsRedirect: true, Mode: routing.Server_TLSOptions_SIMPLE, ServerCertificate: "server.pem", PrivateKey: "key.pem"},
			true},
		{"different",
			&routing.Server_TLSOptions{HttpsRedirect: true, Mode: routing.Server_TLSOptions_SIMPLE, ServerCertificate: "server.pem", PrivateKey: "key.pem"},
			&routing.Server_TLSOptions{HttpsRedirect: false},
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
