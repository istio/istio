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

package tiller

import (
	"sort"

	"k8s.io/helm/pkg/proto/hapi/release"
)

// sortByHookWeight does an in-place sort of hooks by their supplied weight.
func sortByHookWeight(hooks []*release.Hook) []*release.Hook {
	hs := newHookWeightSorter(hooks)
	sort.Sort(hs)
	return hs.hooks
}

type hookWeightSorter struct {
	hooks []*release.Hook
}

func newHookWeightSorter(h []*release.Hook) *hookWeightSorter {
	return &hookWeightSorter{
		hooks: h,
	}
}

func (hs *hookWeightSorter) Len() int { return len(hs.hooks) }

func (hs *hookWeightSorter) Swap(i, j int) {
	hs.hooks[i], hs.hooks[j] = hs.hooks[j], hs.hooks[i]
}

func (hs *hookWeightSorter) Less(i, j int) bool {
	if hs.hooks[i].Weight == hs.hooks[j].Weight {
		return hs.hooks[i].Name < hs.hooks[j].Name
	}
	return hs.hooks[i].Weight < hs.hooks[j].Weight
}
