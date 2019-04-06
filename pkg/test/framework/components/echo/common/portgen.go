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
	nextHTTP  []int
	httpIndex int

	nextGRPC  []int
	grpcIndex int

	nextTCP  []int
	tcpIndex int

	used map[int]struct{}
}

func newPortGenerator() *portGenerator {
	return &portGenerator{
		nextHTTP: []int{80, 8080},
		nextTCP:  []int{90, 9090},
		nextGRPC: []int{70, 7070},
		used:     make(map[int]struct{}),
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
func (g *portGenerator) Next(p model.Protocol) int {
	var next int

	for {
		var nextArray []int
		var index *int
		switch p {
		case model.ProtocolHTTP:
			nextArray = g.nextHTTP
			index = &g.httpIndex
		case model.ProtocolGRPC:
			nextArray = g.nextGRPC
			index = &g.grpcIndex
		default:
			nextArray = g.nextTCP
			index = &g.tcpIndex
		}

		// Get the next value
		next = nextArray[*index]

		// Update the next.
		nextArray[*index]++

		if *index == math.MaxInt16 {
			panic("echo port generator: ran out of ports")
		}

		*index++

		if !g.IsUsed(next) {
			// Mark this port as used.
			g.SetUsed(next)
			break
		}
		// Otherwise, the port was already used, pick another one.
	}

	return next
}
