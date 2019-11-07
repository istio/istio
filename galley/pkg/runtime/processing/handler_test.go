//  Copyright 2019 Istio Authors
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

package processing_test

import (
	"reflect"
	"testing"

	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestHandlerFromFn(t *testing.T) {
	var received resource.Event
	h := processing.HandlerFromFn(func(e resource.Event) {
		received = e
	})

	sent := updateRes1V2()

	h.Handle(sent)

	if !reflect.DeepEqual(received, sent) {
		t.Fatalf("Mismatch: got:%v, expected:%v", received, sent)
	}
}
