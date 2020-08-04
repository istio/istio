// Copyright Istio Authors
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

package status

import (
	"reflect"
	"sync"

	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
)

// Status state for a given resource. This contains both desired and last known status of the resource. It also acts
// as a linked list node for tracking the work of the reconciliation loop.
type status struct {
	key key

	// observedStatus comes from watch events
	observedStatus  interface{}
	observedVersion resource.Version

	// desiredStatus comes from internal processing engine. The desiredStatusVersion is used to ensure that the status
	// will be applied to the right version of the resource.
	desiredStatus        interface{}
	desiredStatusVersion resource.Version

	// next implements a singly-linked list for work tracking purposes.
	next *status
}

type key struct {
	col collection.Name
	res resource.FullName
}

var statusPool = sync.Pool{
	New: func() interface{} {
		return &status{}
	},
}

func getStatusFromPool(k key) *status {
	st := statusPool.Get().(*status)
	st.key = k
	return st
}

func returnStatusToPool(s *status) {
	s.key = key{}
	s.observedStatus = nil
	s.observedVersion = ""
	s.desiredStatus = nil
	s.desiredStatusVersion = ""
}

func (r *status) setObserved(v resource.Version, status interface{}) bool {
	r.observedVersion = v
	r.observedStatus = status

	return r.needsChange()
}

// nolint: unparam
func (r *status) setDesired(v resource.Version, status interface{}) bool {
	r.desiredStatus = status
	r.desiredStatusVersion = v

	return r.needsChange()
}

func (r *status) isEmpty() bool {
	return r.desiredStatus == nil && r.observedStatus == nil
}

func (r *status) isEnqueued() bool {
	return r.next != nil
}

func (r *status) needsChange() bool {
	// Status may be a slice, in which case equality isn't defined, so we use reflect.DeepEqual instead
	return !reflect.DeepEqual(r.observedStatus, r.desiredStatus)
}
