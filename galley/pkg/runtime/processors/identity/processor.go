//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package identity

import (
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("processor", "Galley data processing flow", 0)

// AddProcessor adds processor components for converting an incoming proto directly into enveloped
// form, ready for snapshotting.
func AddProcessor(t resource.TypeURL, b *processing.GraphBuilder) {

	v := processing.NewStoredProjection(t)
	h := processing.HandlerFromFn(func(e resource.Event) {
		switch e.Kind {
		case resource.Added, resource.Updated:
			env, err := resource.ToMcpResource(e.Entry)
			if err != nil {
				scope.Errorf("Error enveloping incoming resource(%v): %v", e.Entry.ID, err)
				return
			}
			v.Set(e.Entry.ID.FullName, env)

		case resource.Deleted:
			v.Remove(e.Entry.ID.FullName)
		}
	})

	b.AddHandler(t, h)
	b.AddProjection(v)
}
