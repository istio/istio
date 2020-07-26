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

package e2e

import (
	"context"
	"io"
	"log"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"google.golang.org/grpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/mixer/pkg/template"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
	attr "istio.io/pkg/attribute"
)

type testData struct {
	name      string
	cfg       string
	behaviors []spyadapter.AdapterBehavior
	templates map[string]template.Info
	attrs     map[string]interface{}

	expectError     error
	expectSetTypes  map[string]interface{}
	expectCalls     []spyadapter.CapturedCall
	expectDirective *istio_mixer_v1.RouteDirective
	expectAttrRefs  []expectedAttrRef
}

type expectedAttrRef struct {
	name      string
	condition istio_mixer_v1.ReferencedAttributes_Condition
	mapkey    string
}

func (tt *testData) run(t *testing.T, variety v1beta1.TemplateVariety, globalCfg string) {
	// Do common setup
	adapterInfos, spyAdapters := constructAdapterInfos(tt.behaviors)

	args := server.DefaultArgs()
	args.APIPort = 0
	args.MonitoringPort = 0
	args.Templates = tt.templates
	args.Adapters = adapterInfos
	var cerr error
	if args.ConfigStore, cerr = storetest.SetupStoreForTest(globalCfg, tt.cfg); cerr != nil {
		t.Fatal(cerr)
	}

	s, err := server.New(args)
	if err != nil {
		t.Fatalf("fail to create mixer: %v", err)
	}

	s.Run()

	defer closeHelper(s)

	conn, err := grpc.Dial(s.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Unable to connect to gRPC server: %v", err)
	}

	client := istio_mixer_v1.NewMixerClient(conn)
	defer closeHelper(conn)

	tt.checkSetTypes(t, spyAdapters)

	switch variety {
	case v1beta1.TEMPLATE_VARIETY_REPORT:
		req := istio_mixer_v1.ReportRequest{
			Attributes: []istio_mixer_v1.CompressedAttributes{
				getAttrBag(tt.attrs)},
		}
		_, err = client.Report(context.Background(), &req)
		tt.checkReturnError(t, err)
		tt.checkCalls(t, spyAdapters)

	case v1beta1.TEMPLATE_VARIETY_CHECK:
		req := istio_mixer_v1.CheckRequest{
			Attributes: getAttrBag(tt.attrs),
		}

		response, err := client.Check(context.Background(), &req)
		tt.checkReturnError(t, err)
		tt.checkCalls(t, spyAdapters)
		tt.checkReferencedAttributes(t, response.Precondition.ReferencedAttributes)

		if tt.expectDirective != nil {
			if !reflect.DeepEqual(tt.expectDirective, response.Precondition.RouteDirective) {
				t.Fatalf("Route directive mismatch:\ngot:\n%v\nwanted:\n%v\n", spew.Sdump(response.Precondition.RouteDirective),
					spew.Sdump(tt.expectDirective))
			}
		}

	default:
		t.Fatalf("Unsupported variety: %v", variety)
	}
}

func (tt *testData) checkReturnError(t *testing.T, err error) {
	if !reflect.DeepEqual(tt.expectError, err) {
		t.Fatalf("Error mismatch: got:'%v', wanted:'%v'", err, tt.expectError)
	}
}

func (tt *testData) checkSetTypes(t *testing.T, adapters []*spyadapter.Adapter) {
	if tt.expectSetTypes == nil {
		return
	}

	// TODO: Handle multiple adapters
	actual := adapters[0].BuilderData.SetTypes
	if !reflect.DeepEqual(actual, tt.expectSetTypes) {
		t.Fatalf("SetTypes Mismatch:\ngot:\n%v\nwanted:\n%v\n", spew.Sdump(actual), spew.Sdump(tt.expectSetTypes))
	}
}

func (tt *testData) checkCalls(t *testing.T, adapters []*spyadapter.Adapter) {
	if tt.expectCalls == nil {
		return
	}

	// TODO: Handle multiple adapters
	actual := adapters[0].HandlerData.CapturedCalls
	if !reflect.DeepEqual(actual, tt.expectCalls) {
		t.Fatalf("Call mismatch:\ngot:\n%v\nwanted:\n%v\n", spew.Sdump(actual), spew.Sdump(tt.expectCalls))
	}
}

func (tt *testData) checkReferencedAttributes(t *testing.T, actual *istio_mixer_v1.ReferencedAttributes) {
	conditions := make(map[string]istio_mixer_v1.ReferencedAttributes_Condition)
	mapkeys := make(map[string]string)
	for _, m := range actual.AttributeMatches {
		switch m.Condition {
		case istio_mixer_v1.EXACT, istio_mixer_v1.ABSENCE, istio_mixer_v1.REGEX:
			// do nothing

		default:
			t.Fatalf("Unexpected condition: %v (%v)", m.Condition, spew.Sdump(m))
		}

		idx := int(-m.Name - 1)
		if idx < 0 && idx > len(actual.Words) {
			t.Fatalf("Out of bounds word reference: %v (%v)", m.Name, spew.Sdump(m))
		}
		name := actual.Words[idx]

		if m.MapKey != 0 {
			idx = int(-m.MapKey - 1)
			if idx < 0 && idx > len(actual.Words) {
				t.Fatalf("Out of bounds word reference from map key: %v (%v)", m.MapKey, spew.Sdump(m))
			}

			mapkey := actual.Words[idx]
			mapkeys[name] = mapkey
		}

		conditions[name] = m.Condition
	}

	if len(conditions) != len(tt.expectAttrRefs) {
		t.Fatalf("Referenced attribute mismatch:\ngot:\n%v\nwanted:\n%v\n", spew.Sdump(actual), spew.Sdump(tt.expectAttrRefs))
	}

	for _, e := range tt.expectAttrRefs {
		acond, ok := conditions[e.name]
		if !ok {
			t.Fatalf("Expected attr ref not found: %v\nactuals:%v\n", e.name, spew.Sdump(e))
		}
		if acond != e.condition {
			t.Fatalf("Expected condition mismatch for '%v': got:%v, wanted:%v", e.name, acond, e.condition)
		}
		amk, ok := mapkeys[e.name]
		if !ok && e.mapkey != "" {
			t.Fatalf("Expected mapkey not found for '%v': %v", e.name, e.mapkey)
		}
		if ok && amk != e.mapkey {
			t.Fatalf("Unexpected mapkey (or mismatch for '%v': got:%v, wanted:%v", e.name, amk, e.mapkey)
		}
	}
}

func closeHelper(c io.Closer) {
	err := c.Close()
	if err != nil {
		log.Fatal(err)
	}
}

func getAttrBag(attrs map[string]interface{}) istio_mixer_v1.CompressedAttributes {
	requestBag := attr.GetMutableBag(nil)
	for k, v := range attrs {
		requestBag.Set(k, v)
	}

	var attrProto istio_mixer_v1.CompressedAttributes
	attribute.ToProto(requestBag, &attrProto, nil, 0)
	return attrProto
}

// constructAdapterInfos constructs spyAdapters for each of the adptBehavior. It returns
// the constructed spyAdapters along with the adapters Info functions.
func constructAdapterInfos(adptBehaviors []spyadapter.AdapterBehavior) ([]adapter.InfoFn, []*spyadapter.Adapter) {
	adapterInfos := make([]adapter.InfoFn, 0)
	spyAdapters := make([]*spyadapter.Adapter, 0)
	for _, b := range adptBehaviors {
		sa := spyadapter.NewSpyAdapter(b)
		spyAdapters = append(spyAdapters, sa)
		adapterInfos = append(adapterInfos, sa.GetAdptInfoFn())
	}
	return adapterInfos, spyAdapters
}
