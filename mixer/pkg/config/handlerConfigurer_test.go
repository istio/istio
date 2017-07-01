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

	"istio.io/mixer/pkg/adapter/config"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	tmpl "istio.io/mixer/pkg/template"
)

type fakeTmplRepo struct {
	tmptExists      bool
	inferTypeError  error
	inferTypeResult proto.Message
}

func newFakeTmplRepo(inferError error, inferResult proto.Message, tmplFound bool) tmpl.Repository {
	return fakeTmplRepo{inferTypeError: inferError, inferTypeResult: inferResult, tmptExists: tmplFound}
}
func (t fakeTmplRepo) GetTemplateInfo(template string) (tmpl.Info, bool) {
	return tmpl.Info{
		InferTypeFn: func(proto.Message, tmpl.TypeEvalFn) (proto.Message, error) {
			return t.inferTypeResult, t.inferTypeError
		},
		CnstrDefConfig: nil,
	}, t.tmptExists
}

type fakeTmplRepo2 struct {
	retErr      error
	shouldPanic bool
	// used to track what is called and later verify in the test.
	trackCallInfo *[][]string
}

func newFakeTmplRepo2(retErr error, shouldPanic bool, trackCallInfo *[][]string) fakeTmplRepo2 {
	return fakeTmplRepo2{retErr: retErr, shouldPanic: shouldPanic, trackCallInfo: trackCallInfo}
}
func (t fakeTmplRepo2) GetTemplateInfo(template string) (tmpl.Info, bool) {
	return tmpl.Info{
		ConfigureTypeFn: func(types map[string]proto.Message, builder *config.HandlerBuilder) error {
			if t.shouldPanic {
				panic("Panic from handler code")
			}
			actInstNames := make([]string, 0)
			for instName := range types {
				actInstNames = append(actInstNames, instName)
			}
			(*t.trackCallInfo) = append(*(t.trackCallInfo), actInstNames)
			return t.retErr
		},
	}, true
}

func TestDispatchTypesToHandlers(t *testing.T) {
	tests := []struct {
		name                    string
		handlers                map[string]*HandlerBuilderInfo
		tmplCnfgrMtdErrRet      error
		tmplCnfgrMtdShouldPanic bool
		hndlrInstsByTmpls       map[string]instancesByTemplate
		infrdTyps               map[string]proto.Message
		wantErr                 string
		expectCallTrackInfo     [][]string
	}{
		{
			name:                "simple",
			tmplCnfgrMtdErrRet:  nil,
			handlers:            map[string]*HandlerBuilderInfo{"hndlr": {handlerBuilder: nil}},
			infrdTyps:           map[string]proto.Message{"inst1": nil},
			hndlrInstsByTmpls:   map[string]instancesByTemplate{"hndlr": map[string][]string{"any": {"inst1"}}},
			wantErr:             "",
			expectCallTrackInfo: [][]string{{"inst1"}},
		},
		{
			name:                "MultiHandlerAndInsts",
			tmplCnfgrMtdErrRet:  nil,
			handlers:            map[string]*HandlerBuilderInfo{"hndlr": {handlerBuilder: nil}, "hndlr2": {handlerBuilder: nil}},
			infrdTyps:           map[string]proto.Message{"inst1": nil, "inst2": nil, "inst3": nil},
			hndlrInstsByTmpls:   map[string]instancesByTemplate{"hndlr": map[string][]string{"any1": {"inst1", "inst2"}, "any2": {"inst3"}}},
			wantErr:             "",
			expectCallTrackInfo: [][]string{{"inst1", "inst2"}, {"inst3"}},
		},
		{
			name:               "ErrorFromAdapterCode",
			tmplCnfgrMtdErrRet: fmt.Errorf("error from adapter configure code"),
			wantErr:            "error from adapter configure code",
			handlers:           map[string]*HandlerBuilderInfo{"hndlr": {handlerBuilder: nil}},
			infrdTyps:          map[string]proto.Message{"inst1": nil},
			hndlrInstsByTmpls:  map[string]instancesByTemplate{"hndlr": map[string][]string{"any": {"inst1"}}},
		},
		{
			name:                    "PanicFromAdapterCodeRecovery",
			tmplCnfgrMtdErrRet:      nil,
			tmplCnfgrMtdShouldPanic: true,
			wantErr:                 "",
			handlers:                map[string]*HandlerBuilderInfo{"hndlr": {handlerBuilder: nil}},
			infrdTyps:               map[string]proto.Message{"inst1": nil},
			hndlrInstsByTmpls:       map[string]instancesByTemplate{"hndlr": map[string][]string{"any": {"inst1"}}},
		},
	}

	ex, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
	for _, tt := range tests {
		actualCallTrackInfo := make([][]string, 0)
		tmplRepo := newFakeTmplRepo2(tt.tmplCnfgrMtdErrRet, tt.tmplCnfgrMtdShouldPanic, &actualCallTrackInfo)
		hc := handlerFactory{typeChecker: ex, tmplRepo: tmplRepo}

		err := hc.dispatch(tt.infrdTyps, tt.hndlrInstsByTmpls, tt.handlers)
		if tt.tmplCnfgrMtdShouldPanic && !tt.handlers["hndlr"].isBroken {
			t.Error("The handler should be marked as broken.")
		}

		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("got err %v\nwant <nil>", err)
			}
			ensureInterfacesInConfigCallsValid(t, tt.expectCallTrackInfo, actualCallTrackInfo)
		} else {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got error %v\nwant %v", err, tt.wantErr)
			}
		}
	}
}

func TestInferTypes(t *testing.T) {
	tests := []struct {
		name         string
		constructors map[string]*pb.Constructor
		tmplRepo     tmpl.Repository
		want         map[string]proto.Message
		wantError    string
	}{
		{
			name:         "SingleCnstr",
			constructors: map[string]*pb.Constructor{"inst1": {"inst1", "tpml1", &empty.Empty{}}},
			tmplRepo:     newFakeTmplRepo(nil, &wrappers.Int32Value{Value: 1}, true),
			want:         map[string]proto.Message{"inst1": &wrappers.Int32Value{Value: 1}},
			wantError:    "",
		},
		{
			name: "MultipleCnstr",
			constructors: map[string]*pb.Constructor{"inst1": {"inst1", "tpml1", &empty.Empty{}},
				"inst2": {"inst2", "tpml1", &empty.Empty{}}},
			tmplRepo: newFakeTmplRepo(nil, &wrappers.Int32Value{Value: 1}, true),
			want:     map[string]proto.Message{"inst1": &wrappers.Int32Value{Value: 1}, "inst2": &wrappers.Int32Value{Value: 1}},
		},
		{
			name:         "ErrorDuringTypeInfr",
			constructors: map[string]*pb.Constructor{"inst1": {"inst1", "tpml1", &empty.Empty{}}},
			tmplRepo:     newFakeTmplRepo(fmt.Errorf("error during type infer"), nil, true),
			want:         nil,
			wantError:    "cannot infer type information",
		},
	}
	ex, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := handlerFactory{typeChecker: ex, tmplRepo: tt.tmplRepo}
			v, err := hc.inferTypes(tt.constructors)
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

func TestDedupeAndGroupInstances(t *testing.T) {
	tests := []struct {
		name         string
		actions      []*pb.Action
		constructors map[string]*pb.Constructor
		handlers     map[string]*HandlerBuilderInfo
		want         map[string]instancesByTemplate
		wantError    string
	}{
		{
			name:         "SimpleNoDedupeNeeded",
			handlers:     map[string]*HandlerBuilderInfo{"hndlr1": nil},
			constructors: map[string]*pb.Constructor{"i1": {"i1", "tpml1", nil}},
			actions:      []*pb.Action{{"hndlr1", []string{"i1"}}},
			want: map[string]instancesByTemplate{
				"hndlr1": map[string][]string{"tpml1": {"i1"}},
			},
		},
		{
			name:     "DedupeAcrossActions",
			handlers: map[string]*HandlerBuilderInfo{"hndlr1": nil},
			constructors: map[string]*pb.Constructor{
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
			constructors: map[string]*pb.Constructor{
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
			constructors: map[string]*pb.Constructor{
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
			constructors: map[string]*pb.Constructor{
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
			v, err := hc.groupByTmpl(tt.actions, tt.constructors, tt.handlers)
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
