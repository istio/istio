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

package krt

import (
	"fmt"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
)

// NewStatusManyCollection builds a ManyCollection that outputs an additional *status* message about the original input.
// For example: with a Service input and []Endpoint output, I might report (ServiceStatus, []Endpoint) where ServiceStatus counts
// the number of attached endpoints.
// Two collections will be output: the status collection and the original output collection.
func NewStatusManyCollection[I controllers.Object, IStatus, O any](
	c Collection[I],
	hf TransformationMultiStatus[I, IStatus, O],
	opts ...CollectionOption,
) (Collection[ObjectWithStatus[I, IStatus]], Collection[O]) {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NewStatusManyCollection[%v,%v,%v]", ptr.TypeName[I](), ptr.TypeName[IStatus](), ptr.TypeName[O]())
	}
	statusOpts := append(opts, WithName(o.name+"/status"))
	statusCh := make(chan struct{})
	statusSynced := channelSyncer{
		name:   o.name + " status",
		synced: statusCh,
	}
	status := NewStaticCollection[ObjectWithStatus[I, IStatus]](statusSynced, nil, statusOpts...)
	// When input is deleted, the transformation function wouldn't run.
	// So we need to handle that explicitly
	cleanupOnRemoval := func(i []Event[I]) {
		for _, e := range i {
			if e.Event == controllers.EventDelete {
				status.DeleteObject(GetKey(e.Latest()))
			}
		}
	}
	primary := newManyCollection[I, O](c, func(ctx HandlerContext, i I) []O {
		st, objs := hf(ctx, i)
		// Create/delete our status objects
		if st == nil {
			status.DeleteObject(GetKey(i))
		} else {
			cs := ObjectWithStatus[I, IStatus]{
				Obj:    i,
				Status: *st,
			}
			status.ConditionalUpdateObject(cs)
		}
		return objs
	}, o, cleanupOnRemoval)
	go func() {
		// Status collection is not synced until the parent is synced.
		// We could almost just pass the primary.Syncer(), but that is circular so doesn't really work.
		if primary.WaitUntilSynced(o.stop) {
			close(statusCh)
		}
	}()

	return status, primary
}

func NewStatusCollection[I controllers.Object, IStatus, O any](
	c Collection[I],
	hf TransformationSingleStatus[I, IStatus, O],
	opts ...CollectionOption,
) (Collection[ObjectWithStatus[I, IStatus]], Collection[O]) {
	hm := func(ctx HandlerContext, i I) (*IStatus, []O) {
		status, res := hf(ctx, i)
		if res == nil {
			return status, nil
		}
		return status, []O{*res}
	}
	return NewStatusManyCollection(c, hm, opts...)
}

type StatusCollection[I controllers.Object, IStatus any] = Collection[ObjectWithStatus[I, IStatus]]

type ObjectWithStatus[I controllers.Object, IStatus any] struct {
	Obj    I
	Status IStatus
}

func (c ObjectWithStatus[I, IStatus]) ResourceName() string {
	return GetKey(c.Obj)
}

func (c ObjectWithStatus[I, IStatus]) Equals(o ObjectWithStatus[I, IStatus]) bool {
	// Check the resource is identical.
	// Typically, this will generate more events than needed.
	// The object may change for reasons we don't care about, such as things not impacting generation (labels, annotations),
	// or other writes to status.
	// Downstream of the collection, a user should suppress events where the status in the cluster (o.Obj) and the desired status (o.Status)
	// are not equal.
	// In theory, we could do this here but there is not a good way to get the o.Obj.Status since there is no common interface
	// for GetStatus()
	return Equal(c.Obj, o.Obj) && Equal(c.Status, o.Status)
}
