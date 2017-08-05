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

package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/mixer/pkg/adapter"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	tmpl "istio.io/mixer/pkg/template"
)

type fakeTmplRepo struct {
	exists     bool
	err        error
	typeResult proto.Message
}

func newFakeTmplRepo(err error, result proto.Message) tmpl.Repository {
	return fakeTmplRepo{err: err, typeResult: result, exists: true}
}

func (t fakeTmplRepo) GetTemplateInfo(template string) (tmpl.Info, bool) {
	return tmpl.Info{
		InferType: func(proto.Message, tmpl.TypeEvalFn) (proto.Message, error) {
			return t.typeResult, t.err
		},
		CtrCfg: nil,
	}, t.exists
}

func (t fakeTmplRepo) SupportsTemplate(hndlrBuilder adapter.HandlerBuilder, s string) (bool, string) {
	// always succeed
	return true, ""
}

type instancesPerCall [][]string
type fakeTmplRepo2 struct {
	err       error
	panicTmpl string
	// used to track what is called and later verify in the test.
	trace *instancesPerCall
}

func newFakeTmplRepo2(retErr error, cnfgTypePanicsForTmpl string, trackInstancesPerCall *instancesPerCall) fakeTmplRepo2 {
	return fakeTmplRepo2{err: retErr, panicTmpl: cnfgTypePanicsForTmpl, trace: trackInstancesPerCall}
}
func (t fakeTmplRepo2) GetTemplateInfo(template string) (tmpl.Info, bool) {
	return tmpl.Info{
		ConfigureType: func(types map[string]proto.Message, builder *adapter.HandlerBuilder) error {
			instances := make([]string, 0)
			for instance := range types {
				instances = append(instances, instance)
			}
			(*t.trace) = append(*(t.trace), instances)
			if t.panicTmpl == template {
				panic("Panic from handler code")
			}
			return t.err
		},
	}, true
}

func (t fakeTmplRepo2) SupportsTemplate(hndlrBuilder adapter.HandlerBuilder, s string) (bool, string) {
	// always succeed
	return true, ""
}

func TestDispatchToHandlers(t *testing.T) {
	tests := []struct {
		name                string
		handlers            map[string]*HandlerBuilderInfo
		instancesByTemplate map[string]instancesByTemplate
		infrdTyps           map[string]proto.Message

		cnfgTypeErr               error
		wantErr                   string
		wantTrackInstancesPerCall [][]string
	}{
		{
			name:                      "simple",
			handlers:                  map[string]*HandlerBuilderInfo{"hndlr": {handlerBuilder: nil}},
			infrdTyps:                 map[string]proto.Message{"inst1": nil},
			instancesByTemplate:       map[string]instancesByTemplate{"hndlr": map[string][]string{"any": {"inst1"}}},
			cnfgTypeErr:               nil,
			wantTrackInstancesPerCall: [][]string{{"inst1"}},
		},
		{
			name:                      "MultiHandlerAndInsts",
			handlers:                  map[string]*HandlerBuilderInfo{"hndlr": {handlerBuilder: nil}, "hndlr2": {handlerBuilder: nil}},
			infrdTyps:                 map[string]proto.Message{"inst1": nil, "inst2": nil, "inst3": nil},
			instancesByTemplate:       map[string]instancesByTemplate{"hndlr": map[string][]string{"any1": {"inst1", "inst2"}, "any2": {"inst3"}}},
			cnfgTypeErr:               nil,
			wantTrackInstancesPerCall: [][]string{{"inst1", "inst2"}, {"inst3"}},
		},
		{
			name:                "ErrorFromAdapterCode",
			handlers:            map[string]*HandlerBuilderInfo{"hndlr": {handlerBuilder: nil}},
			infrdTyps:           map[string]proto.Message{"inst1": nil},
			instancesByTemplate: map[string]instancesByTemplate{"hndlr": map[string][]string{"any": {"inst1"}}},
			cnfgTypeErr:         fmt.Errorf("error from adapter configure code"),
			wantErr:             "error from adapter configure code",
		},
	}

	ex, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
	for _, tt := range tests {
		actualCallTrackInfo := make(instancesPerCall, 0)
		tmplRepo := newFakeTmplRepo2(tt.cnfgTypeErr, "", &actualCallTrackInfo)
		hc := handlerFactory{typeChecker: ex, tmplRepo: tmplRepo}

		err := hc.dispatch(tt.infrdTyps, tt.instancesByTemplate, tt.handlers)

		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("got err %v\nwant <nil>", err)
			}
			ensureInterfacesInConfigCallsValid(t, tt.wantTrackInstancesPerCall, actualCallTrackInfo)
		} else {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got error %v\nwant %v", err, tt.wantErr)
			}
		}
	}
}

func TestDispatchToHandlersPanicRecover(t *testing.T) {
	type TestData struct {
		name                string
		handlers            map[string]*HandlerBuilderInfo
		instancesByTemplate map[string]instancesByTemplate
		infrdTyps           map[string]proto.Message

		cnfgTypeErr           error
		cnfgTypePanicsForTmpl string
		wantErr               string
		wantCallTrackInfo     [][]string
	}

	tmpCausingPanic := "BadTmplCausePanic"
	badHndlr := "badHndlr"
	goodHndlr1 := "goodHndlr1"
	goodHndlr2 := "goodHndlr2"

	handlers := map[string]*HandlerBuilderInfo{
		badHndlr:   {handlerBuilder: nil},
		goodHndlr1: {handlerBuilder: nil},
		goodHndlr2: {handlerBuilder: nil},
	}
	infrdTypes := map[string]proto.Message{"inst1": nil, "inst2": nil, "inst3": nil}

	instancesByTemplate := map[string]instancesByTemplate{
		badHndlr:   map[string][]string{"BadTmplCausePanic": {"inst1"}},
		goodHndlr1: map[string][]string{"any": {"inst2"}},
		goodHndlr2: map[string][]string{"any": {"inst3"}},
	}
	wantcallTrackInfo := [][]string{{"inst1"}, {"inst2"}, {"inst3"}}

	tests := []TestData{
		{
			name:                  "PanicNoError",
			handlers:              handlers,
			infrdTyps:             infrdTypes,
			instancesByTemplate:   instancesByTemplate,
			cnfgTypePanicsForTmpl: tmpCausingPanic,
			wantCallTrackInfo:     wantcallTrackInfo,
			cnfgTypeErr:           nil,
		},
		{
			name:                  "PanicAfterError",
			handlers:              handlers,
			infrdTyps:             infrdTypes,
			instancesByTemplate:   instancesByTemplate,
			cnfgTypePanicsForTmpl: tmpCausingPanic,
			wantCallTrackInfo:     wantcallTrackInfo,
			cnfgTypeErr:           fmt.Errorf("error from configure-type"),
			wantErr:               "error from configure-type",
		},
	}

	for _, tt := range tests {
		ex, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
		actualCallTrackInfo := make(instancesPerCall, 0)
		tmplRepo := newFakeTmplRepo2(tt.cnfgTypeErr, tt.cnfgTypePanicsForTmpl, &actualCallTrackInfo)
		hc := handlerFactory{typeChecker: ex, tmplRepo: tmplRepo}

		err := hc.dispatch(tt.infrdTyps, tt.instancesByTemplate, tt.handlers)

		if !tt.handlers[badHndlr].isBroken || tt.handlers[goodHndlr1].isBroken || tt.handlers[goodHndlr2].isBroken {
			t.Errorf("The handler %s should be marked as broken. Handlers %s and %s should not be broken", badHndlr, goodHndlr1, goodHndlr2)
		}

		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("got err %v\nwant <nil>", err)
			}
			ensureInterfacesInConfigCallsValid(t, tt.wantCallTrackInfo, actualCallTrackInfo)
		} else {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got error %v\nwant %v", err, tt.wantErr)
			}
		}
	}
}

func TestInferTypes(t *testing.T) {
	tests := []struct {
		name      string
		cnstrs    map[string]*pb.Instance
		tmplRepo  tmpl.Repository
		want      map[string]proto.Message
		wantError string
	}{
		{
			name:     "SingleCnstr",
			cnstrs:   map[string]*pb.Instance{"inst1": {"inst1", "tpml1", &empty.Empty{}}},
			tmplRepo: newFakeTmplRepo(nil, &wrappers.Int32Value{Value: 1}),
			want:     map[string]proto.Message{"inst1": &wrappers.Int32Value{Value: 1}},
		},
		{
			name: "MultipleCnstr",
			cnstrs: map[string]*pb.Instance{"inst1": {"inst1", "tpml1", &empty.Empty{}},
				"inst2": {"inst2", "tpml1", &empty.Empty{}}},
			tmplRepo: newFakeTmplRepo(nil, &wrappers.Int32Value{Value: 1}),
			want:     map[string]proto.Message{"inst1": &wrappers.Int32Value{Value: 1}, "inst2": &wrappers.Int32Value{Value: 1}},
		},
		{
			name:      "ErrorDuringTypeInfr",
			cnstrs:    map[string]*pb.Instance{"inst1": {"inst1", "tpml1", &empty.Empty{}}},
			tmplRepo:  newFakeTmplRepo(fmt.Errorf("error during type infer"), nil),
			want:      nil,
			wantError: "cannot infer type information",
		},
	}
	ex, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := handlerFactory{typeChecker: ex, tmplRepo: tt.tmplRepo}
			v, err := hc.inferTypes(tt.cnstrs)
			if tt.wantError == "" {
				if err != nil {
					t.Errorf("got err %v\nwant <nil>", err)
				}
				if !reflect.DeepEqual(tt.want, v) {
					t.Errorf("got %v\nwant %v", v, tt.want)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("got error %v\nwant %v", err, tt.wantError)
				}
			}

		})
	}
}

func TestGroupByTmpl(t *testing.T) {
	tests := []struct {
		name      string
		actions   []*pb.Action
		instances map[string]*pb.Instance
		handlers  map[string]*HandlerBuilderInfo
		want      map[string]instancesByTemplate
		wantError string
	}{
		{
			name:      "SimpleNoDedupeNeeded",
			handlers:  map[string]*HandlerBuilderInfo{"hndlr1": nil},
			instances: map[string]*pb.Instance{"i1": {"i1", "tpml1", nil}},
			actions:   []*pb.Action{{"hndlr1", []string{"i1"}}},
			want: map[string]instancesByTemplate{
				"hndlr1": map[string][]string{"tpml1": {"i1"}},
			},
		},
		{
			name:     "DedupeAcrossActions",
			handlers: map[string]*HandlerBuilderInfo{"hndlr1": nil},
			instances: map[string]*pb.Instance{
				"repeatInst": {"repeatInst", "tpml1", nil},
				"inst2":      {"inst2", "tpml1", nil}},
			actions: []*pb.Action{
				{"hndlr1", []string{"repeatInst"}},
				{"hndlr1", []string{"repeatInst", "inst2"}}},
			want: map[string]instancesByTemplate{
				"hndlr1": map[string][]string{"tpml1": {"repeatInst", "inst2"}},
			},
		},
		{
			name:     "DedupeWithinAction",
			handlers: map[string]*HandlerBuilderInfo{"hndlr1": nil},
			instances: map[string]*pb.Instance{
				"repeatInst": {"repeatInst", "tpml1", nil},
				"inst2":      {"inst2", "tpml1", nil}},
			actions: []*pb.Action{
				{"hndlr1", []string{"repeatInst", "repeatInst"}},
				{"hndlr1", []string{"inst2"}}},
			want: map[string]instancesByTemplate{
				"hndlr1": map[string][]string{"tpml1": {"repeatInst", "inst2"}},
			},
		},
		{
			name:     "MultipleTemplates",
			handlers: map[string]*HandlerBuilderInfo{"hndlr1": nil},
			instances: map[string]*pb.Instance{
				"inst1tmplA": {"inst1tmplA", "tmplA", nil},
				"inst2tmplA": {"inst2tmplA", "tmplA", nil},

				"inst3tmplB": {"inst3tmplB", "tmplB", nil},
				"inst4tmplB": {"inst4tmplB", "tmplB", nil},
				"inst5tmplB": {"inst5tmplB", "tmplB", nil},
			},
			actions: []*pb.Action{
				{"hndlr1", []string{"inst2tmplA", "inst4tmplB", "inst5tmplB", "inst1tmplA", "inst3tmplB"}},
			},
			want: map[string]instancesByTemplate{
				"hndlr1": map[string][]string{"tmplA": {"inst2tmplA", "inst1tmplA"}, "tmplB": {"inst3tmplB", "inst5tmplB", "inst4tmplB"}},
			},
		},
		{
			name:     "UnionAcrossActionsWithMultipleTemplates",
			handlers: map[string]*HandlerBuilderInfo{"hndlr1": nil, "hndlr2": nil},
			instances: map[string]*pb.Instance{
				"inst1tmplA": {"inst1tmplA", "tmplA", nil},
				"inst2tmplA": {"inst2tmplA", "tmplA", nil},

				"inst3tmplB": {"inst3tmplB", "tmplB", nil},
				"inst4tmplB": {"inst4tmplB", "tmplB", nil},
				"inst5tmplB": {"inst5tmplB", "tmplB", nil},
			},
			actions: []*pb.Action{
				{"hndlr1", []string{"inst1tmplA", "inst3tmplB"}},
				{"hndlr1", []string{"inst2tmplA", "inst4tmplB", "inst5tmplB"}},
				{"hndlr2", []string{"inst2tmplA", "inst4tmplB", "inst5tmplB", "inst1tmplA", "inst3tmplB"}},
			},
			want: map[string]instancesByTemplate{
				"hndlr1": map[string][]string{"tmplA": {"inst2tmplA", "inst1tmplA"}, "tmplB": {"inst3tmplB", "inst5tmplB", "inst4tmplB"}},
				"hndlr2": map[string][]string{"tmplA": {"inst2tmplA", "inst1tmplA"}, "tmplB": {"inst3tmplB", "inst5tmplB", "inst4tmplB"}},
			},
		},
	}
	ex, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			hc := handlerFactory{typeChecker: ex, tmplRepo: nil}
			v, err := hc.groupByTmpl(tt.actions, tt.instances, tt.handlers)
			if tt.wantError == "" {
				if err != nil {
					t.Errorf("got err %v\nwant <nil>", err)
				}
				if !deepEqualsOrderIndependent(tt.want, v) {
					t.Errorf("got %v\nwant %v", v, tt.want)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("got error %v\nwant %v", err, tt.wantError)
				}
			}

		})
	}

}

func deepEqualsOrderIndependent(expected map[string]instancesByTemplate, actual map[string]instancesByTemplate) bool {
	if len(expected) != len(actual) {
		return false
	}

	for k, exTmplCnstMap := range expected {
		actTmplCnstMap, ok := actual[k]
		if !ok {
			return false
		}

		var exInstNamesByTmpl = exTmplCnstMap
		var actInstNamesByTmpl = actTmplCnstMap
		if len(exInstNamesByTmpl) != len(actInstNamesByTmpl) {
			return false
		}

		for exTmplName, exInsts := range exInstNamesByTmpl {
			var actInsts []string
			var ok bool
			if actInsts, ok = actInstNamesByTmpl[exTmplName]; !ok {
				return false
			}

			if len(exInsts) != len(actInsts) {
				return false
			}

			for _, exInst := range exInsts {
				if !contains(actInsts, exInst) {
					return false
				}
			}
		}
	}
	return true
}

func ensureInterfacesInConfigCallsValid(t *testing.T, expectCallTrackInfo [][]string, actualCallTrackInfo [][]string) {
	if len(expectCallTrackInfo) != len(actualCallTrackInfo) {
		t.Errorf("got call info for dispatchTypesToHandlers = %v\nwant %v", actualCallTrackInfo, expectCallTrackInfo)
	}
	for _, v := range expectCallTrackInfo {
		found := false
		sort.Strings(v)
		for _, m := range actualCallTrackInfo {
			sort.Strings(m)
			if reflect.DeepEqual(v, m) {
				found = true
			}
		}
		if !found {
			t.Errorf("got call info for dispatchTypesToHandlers = %v\nwant %v", actualCallTrackInfo, expectCallTrackInfo)
		}
	}
}
