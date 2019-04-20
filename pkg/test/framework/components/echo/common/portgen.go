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

package common

import (
	"math"

	"istio.io/istio/pilot/pkg/model"
)

const (
	httpBase  = 80
	httpsBase = 443
	grpcBase  = 7070
	tcpBase   = 9090
)

// portGenerators creates a set of generators for service and instance ports.
type portGenerators struct {
	Service  *portGenerator
	Instance *portGenerator
}

// newPortGenerators creates a new set of port generators.
func newPortGenerators() *portGenerators {
	return &portGenerators{
		Service:  newPortGenerator(),
		Instance: newPortGenerator(),
	}
}

// portGenerator is a utility that generates reasonable default port values
// for a given protocol.
type portGenerator struct {
	next map[model.Protocol]int
	used map[int]struct{}
}

func newPortGenerator() *portGenerator {
	return &portGenerator{
		next: map[model.Protocol]int{
			model.ProtocolHTTP:    httpBase,
			model.ProtocolHTTPS:   httpsBase,
			model.ProtocolTLS:     httpsBase,
			model.ProtocolTCP:     tcpBase,
			model.ProtocolGRPCWeb: grpcBase,
			model.ProtocolGRPC:    grpcBase,
			model.ProtocolMongo:   tcpBase,
			model.ProtocolMySQL:   tcpBase,
			model.ProtocolRedis:   tcpBase,
			model.ProtocolUDP:     tcpBase,
		},
		used: make(map[int]struct{}),
	}
}

// SetUsed marks the given port as used, so that it will not be assigned by the
// generator.
func (g *portGenerator) SetUsed(port int) *portGenerator {
	g.used[port] = struct{}{}
	return g
}

// IsUsed indicates if the given port has already been used.
func (g *portGenerator) IsUsed(port int) bool {
	_, ok := g.used[port]
	return ok
}

// Next assigns the next port for the given protocol.
func (g *portGenerator) Next(protocol model.Protocol) int {
	for {
		v := g.next[protocol]

		if v == 0 {
			panic("echo port generator: unsupported protocol " + protocol)
		}

		if v == math.MaxInt16 {
			panic("echo port generator: ran out of ports")
		}

		g.next[protocol] = v + 1

		if g.IsUsed(v) {
			continue
		}

		// Mark this port as used.
		g.SetUsed(v)
		return v
	}
}
