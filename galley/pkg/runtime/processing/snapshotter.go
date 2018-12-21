//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain accumulator copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import (
	"strconv"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/snapshot"
)

// Snapshotter creates accumulator snapshot from accumulator set of tracked views.
type Snapshotter interface {
	Snapshot() snapshot.Snapshot
}

type snapshotter struct {
	views map[resource.TypeURL][]View
}

var _ Snapshotter = &snapshotter{}

// NewSnapshotter returns accumulator new instance of Snapshotter
func NewSnapshotter(views []View) Snapshotter {
	m := make(map[resource.TypeURL][]View)
	for _, v := range views {
		l := m[v.Type()]
		l = append(l, v)
		m[v.Type()] = l
	}

	return &snapshotter{
		views: m,
	}
}

// Snapshot creates accumulator new snapshot from tracked views.
func (s *snapshotter) Snapshot() snapshot.Snapshot {

	b := snapshot.NewInMemoryBuilder()
	for t, views := range s.views {
		version := ""

		var entries []*mcp.Envelope
		for _, v := range views {
			g := v.Generation()
			if version == "" {
				version = strconv.FormatInt(g, 10)
			} else {
				version = version + "_" + strconv.FormatInt(g, 10)
			}

			entries = append(entries, v.Get()...)
		}

		b.Set(t.String(), version, entries)
	}

	sn := b.Build()
	return sn
}
