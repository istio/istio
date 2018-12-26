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

package flow

import (
	"reflect"
	"testing"

	mcp "istio.io/api/mcp/v1alpha1"
)

func TestPipeline_Basic(t *testing.T) {
	b := NewPipelineBuilder()

	fh := &fakeHandler{
		result: true,
	}
	b.AddHandler(emptyInfo.TypeURL, fh)

	v := &fakeView{
		typeURL: emptyInfo.TypeURL,
	}
	b.AddView(v)

	p := b.Build()

	changed := p.Handle(addRes1V1())
	if !changed {
		t.Fatalf("should have changed")
	}

	if len(fh.evts) != 1 || !reflect.DeepEqual(fh.evts[0], addRes1V1()) {
		t.Fatalf("Unexpected accumulated events: %v", fh.evts)
	}

	v.items = []*mcp.Envelope{
		{
			Metadata: &mcp.Metadata{
				Name:    "n1",
				Version: "v1",
			},
		},
	}

	sn := p.Snapshot()
	actual := sn.Resources(emptyInfo.TypeURL.String())

	if !reflect.DeepEqual(v.items, actual) {
		t.Fatalf("mismatch: got:%v, wanted:%v", actual, v.items)
	}
}
