// Copyright 2019 The OpenZipkin Authors
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

package zipkin

import (
	"net"
	"strconv"
	"strings"

	"github.com/openzipkin/zipkin-go/model"
)

// NewEndpoint creates a new endpoint given the provided serviceName and
// hostPort.
func NewEndpoint(serviceName string, hostPort string) (*model.Endpoint, error) {
	e := &model.Endpoint{
		ServiceName: serviceName,
	}

	if hostPort == "" || hostPort == ":0" {
		if serviceName == "" {
			// if all properties are empty we should not have an Endpoint object.
			return nil, nil
		}
		return e, nil
	}

	if strings.IndexByte(hostPort, ':') < 0 {
		hostPort += ":0"
	}

	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return nil, err
	}

	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, err
	}
	e.Port = uint16(p)

	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	for i := range addrs {
		addr := addrs[i].To4()
		if addr == nil {
			// IPv6 - 16 bytes
			if e.IPv6 == nil {
				e.IPv6 = addrs[i].To16()
			}
		} else {
			// IPv4 - 4 bytes
			if e.IPv4 == nil {
				e.IPv4 = addr
			}
		}
		if e.IPv4 != nil && e.IPv6 != nil {
			// Both IPv4 & IPv6 have been set, done...
			break
		}
	}

	return e, nil
}
