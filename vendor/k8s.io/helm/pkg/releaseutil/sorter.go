/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package releaseutil // import "k8s.io/helm/pkg/releaseutil"

import (
	"sort"

	rspb "k8s.io/helm/pkg/proto/hapi/release"
)

type sorter struct {
	list []*rspb.Release
	less func(int, int) bool
}

func (s *sorter) Len() int           { return len(s.list) }
func (s *sorter) Less(i, j int) bool { return s.less(i, j) }
func (s *sorter) Swap(i, j int)      { s.list[i], s.list[j] = s.list[j], s.list[i] }

// Reverse reverses the list of releases sorted by the sort func.
func Reverse(list []*rspb.Release, sortFn func([]*rspb.Release)) {
	sortFn(list)
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
}

// SortByName returns the list of releases sorted
// in lexicographical order.
func SortByName(list []*rspb.Release) {
	s := &sorter{list: list}
	s.less = func(i, j int) bool {
		ni := s.list[i].Name
		nj := s.list[j].Name
		return ni < nj
	}
	sort.Sort(s)
}

// SortByDate returns the list of releases sorted by a
// release's last deployed time (in seconds).
func SortByDate(list []*rspb.Release) {
	s := &sorter{list: list}

	s.less = func(i, j int) bool {
		ti := s.list[i].Info.LastDeployed.Seconds
		tj := s.list[j].Info.LastDeployed.Seconds
		return ti < tj
	}
	sort.Sort(s)
}

// SortByRevision returns the list of releases sorted by a
// release's revision number (release.Version).
func SortByRevision(list []*rspb.Release) {
	s := &sorter{list: list}
	s.less = func(i, j int) bool {
		vi := s.list[i].Version
		vj := s.list[j].Version
		return vi < vj
	}
	sort.Sort(s)
}

// SortByChartName sorts the list of releases by a
// release's chart name in lexicographical order.
func SortByChartName(list []*rspb.Release) {
	s := &sorter{list: list}
	s.less = func(i, j int) bool {
		chi := s.list[i].Chart
		chj := s.list[j].Chart

		ni := ""
		if chi != nil && chi.Metadata != nil {
			ni = chi.Metadata.Name
		}

		nj := ""
		if chj != nil && chj.Metadata != nil {
			nj = chj.Metadata.Name
		}

		return ni < nj
	}
	sort.Sort(s)
}
