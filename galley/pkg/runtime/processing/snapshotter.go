//  Copyright 2019 Istio Authors
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

type snapshotter struct {
	projections map[resource.TypeURL][]Projection
}

// newSnapshotter returns accumulator new instance of snapshotter
func newSnapshotter(projections []Projection) *snapshotter {
	m := make(map[resource.TypeURL][]Projection)
	for _, v := range projections {
		l := m[v.Type()]
		l = append(l, v)
		m[v.Type()] = l
	}

	return &snapshotter{
		projections: m,
	}
}

// snapshot creates accumulator new snapshot from tracked projections.
func (s *snapshotter) snapshot(urls []resource.TypeURL) snapshot.Snapshot {

	b := snapshot.NewInMemoryBuilder()

	for _, u := range urls {
		projections, found := s.projections[u]
		if !found {
			scope.Errorf("TypeURL not found for snapshotting: %p", u)
			continue
		}
		version := ""

		var entries []*mcp.Resource
		for _, p := range projections {
			g := p.Generation()
			if version == "" {
				version = strconv.FormatInt(g, 10)
			} else {
				version = version + "_" + strconv.FormatInt(g, 10)
			}

			entries = append(entries, p.Get()...)
		}

		b.Set(u.String(), version, entries)
	}

	sn := b.Build()
	return sn
}
