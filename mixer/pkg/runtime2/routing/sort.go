// Copyright 2017 Istio Authors
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

package routing

import "sort"

// sortByHandlerName the contents of the handlerNamesByID for stable ordering.
func (e *handlerEntries) sortByHandlerName(handlerNamesByID map[uint32]string) {
	sorter := sortByHandlers{
		e:                e,
		handlerNamesByID: handlerNamesByID,
	}

	sort.Sort(&sorter)
}

// sorts handlerNamesByID.entries by handler names.
type sortByHandlers struct {
	e                *handlerEntries
	handlerNamesByID map[uint32]string
}

// Len is part of sortByHandlerName.Interface.
func (s *sortByHandlers) Len() int {
	return len(s.e.entries)
}

// Less is part of sortByHandlerName.Interface.
func (s *sortByHandlers) Less(i, j int) bool {
	return s.handlerNamesByID[s.e.entries[i].ID] < s.handlerNamesByID[s.e.entries[j].ID]
}

// Swap is part of sortByHandlerName.Interface.
func (s *sortByHandlers) Swap(i, j int) {
	s.e.entries[i], s.e.entries[j] = s.e.entries[j], s.e.entries[i]
}

func (h *HandlerEntry) sortByMatchText(matches map[uint32]string) {
	sorter := sortByMatchText{
		h: h,
		matches: matches,
	}

	sort.Stable(&sorter)
}

type sortByMatchText struct {
	h *HandlerEntry
	matches map[uint32]string
}

// Len is part of sortByHandlerName.Interface.
func (s *sortByMatchText) Len() int {
	return len(s.h.Inputs)
}

// Swap is part of sortByHandlerName.Interface.
func (s *sortByMatchText) Swap(i, j int) {
	s.h.Inputs[i], s.h.Inputs[j] = s.h.Inputs[j], s.h.Inputs[i]
}

// Less is part of sortByHandlerName.Interface.
func (s *sortByMatchText) Less(i, j int) bool {
	return s.matches[s.h.Inputs[i].ID] < s.matches[s.h.Inputs[j].ID]
}

// sorts InputSet.Builders by instance names.
func (i *InputSet) sortByInstanceName(instanceNames []string) {
	sorter := sortByInstanceName{
		i: i,
		instanceNames: instanceNames,
	}
	sort.Stable(&sorter)
}

type sortByInstanceName struct {
	i               *InputSet
	instanceNames   []string
}

// Len is part of sortByHandlerName.Interface.
func (s *sortByInstanceName) Len() int {
	return len(s.i.Builders)
}

// Swap is part of sortByHandlerName.Interface.
func (s *sortByInstanceName) Swap(i, j int) {
	s.i.Builders[i], s.i.Builders[j] = s.i.Builders[j], s.i.Builders[i]
	s.instanceNames[i], s.instanceNames[j] = s.instanceNames[j], s.instanceNames[i]

	if len(s.i.Mappers) != 0 {
		s.i.Mappers[i], s.i.Mappers[j] = s.i.Mappers[j], s.i.Mappers[i]
	}
}

// Less is part of sortByHandlerName.Interface.
func (s *sortByInstanceName) Less(i, j int) bool {
	return s.instanceNames[i] < s.instanceNames[j]
}
