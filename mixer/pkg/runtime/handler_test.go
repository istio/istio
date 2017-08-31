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

package runtime

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/mixer/pkg/adapter"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/handler"
	tmpl "istio.io/mixer/pkg/template"
)

type fakeTmplRepo struct {
	infrErr    error
	cnfgrErr   error
	cnfgrPanic string
	typeResult proto.Message

	cnfgMtdCallInfo map[string]map[string]proto.Message // templateName - > map[instName]InferredType (proto.Message)
}

func (t fakeTmplRepo) GetTemplateInfo(template string) (tmpl.Info, bool) {
	return tmpl.Info{
		InferType: func(proto.Message, tmpl.TypeEvalFn) (proto.Message, error) {
			return t.typeResult, t.infrErr
		},
		ConfigureType: func(types map[string]proto.Message, builder *adapter.HandlerBuilder) error {
			if t.cnfgrPanic != "" {
				panic(t.cnfgrPanic)
			}
			if t.cnfgMtdCallInfo != nil {
				t.cnfgMtdCallInfo[template] = types
			}

			return t.cnfgrErr
		},
	}, true
}

func (t fakeTmplRepo) SupportsTemplate(hndlrBuilder adapter.HandlerBuilder, s string) (bool, string) {
	// always succeed
	return true, ""
}

type fakeHndlrBldr struct {
	bldPanic string
	bldErr   error
}
type fakeHndlr struct {
	createdWithCnfg adapter.Config
}

func (f fakeHndlr) Close() error {
	return nil
}

func (f fakeHndlrBldr) Build(cnfg adapter.Config, env adapter.Env) (adapter.Handler, error) {
	if f.bldPanic != "" {
		panic(f.bldPanic)
	}

	return fakeHndlr{createdWithCnfg: cnfg}, f.bldErr
}

func TestBuild_Error(t *testing.T) {
	tests := []struct {
		name string

		instsCnfg []*pb.Instance
		hndlrCnfg *pb.Handler

		tmplRepo     tmpl.Repository
		hndlrBuilder adapter.HandlerBuilder

		// want      proto.Message
		wantError string
	}{
		{
			name:         "ErrorNilCreatedHandlerBuilder",
			hndlrBuilder: nil,
			wantError:    "nil HandlerBuilder instantiated for adapter 'a1' in handler config 'h1'",

			hndlrCnfg: &pb.Handler{Name: "h1", Adapter: "a1"},
		},
		{
			name:      "ErrorConfigureXXXX",
			tmplRepo:  fakeTmplRepo{cnfgrErr: fmt.Errorf("FOOBAR ERROR")},
			wantError: "for mesh function name 'tpml1': FOOBAR ERROR",

			instsCnfg:    []*pb.Instance{{"inst1", "tpml1", &empty.Empty{}}},
			hndlrCnfg:    &pb.Handler{Name: "h1", Adapter: "a1", Params: &empty.Empty{}},
			hndlrBuilder: fakeHndlrBldr{},
		},

		{
			name:     "PanicConfigureXXXX",
			tmplRepo: fakeTmplRepo{cnfgrPanic: "FOOBAR PANIC"},
			wantError: "handler panicked with 'FOOBAR PANIC' when trying to configure the " +
				"associated adapter. Please remove the handler or fix the configuration",

			instsCnfg:    []*pb.Instance{{"inst1", "tpml1", &empty.Empty{}}},
			hndlrCnfg:    &pb.Handler{Name: "h1", Adapter: "a1"},
			hndlrBuilder: fakeHndlrBldr{},
		},

		{
			name:         "ErrorAdptBuildXXXX",
			hndlrBuilder: fakeHndlrBldr{bldErr: fmt.Errorf("FOOBAR ERROR from HandlerBuidler build")},
			wantError:    "cannot configure adapter 'a1' in handler config 'h1': FOOBAR ERROR from HandlerBuidler build",

			tmplRepo:  fakeTmplRepo{},
			instsCnfg: []*pb.Instance{{"inst1", "tpml1", &empty.Empty{}}},
			hndlrCnfg: &pb.Handler{Name: "h1", Adapter: "a1", Params: &empty.Empty{}},
		},

		{
			name:         "PanicAdptBuild",
			hndlrBuilder: fakeHndlrBldr{bldPanic: "FOOBAR ERROR panic from HandlerBuidler build"},
			wantError: "handler panicked with 'FOOBAR ERROR panic from HandlerBuidler build' when trying to " +
				"configure the associated adapter",

			tmplRepo:  fakeTmplRepo{},
			hndlrCnfg: &pb.Handler{Name: "h1", Adapter: "a1", Params: &empty.Empty{}},
		},

		{
			name:      "ErrorTypeInferError",
			tmplRepo:  fakeTmplRepo{infrErr: fmt.Errorf("FOOBAR ERROR")},
			wantError: "cannot infer type information from params in instance 'inst1': FOOBAR ERROR",

			instsCnfg:    []*pb.Instance{{"inst1", "tpml1", &empty.Empty{}}},
			hndlrCnfg:    &pb.Handler{Name: "h1", Adapter: "a1"},
			hndlrBuilder: fakeHndlrBldr{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			bldrInfoFinder := func(name string) (*handler.Info, bool) {
				return &handler.Info{CreateHandlerBuilder: func() adapter.HandlerBuilder { return tt.hndlrBuilder }}, true
			}

			hf := NewHandlerFactory(tt.tmplRepo, nil, nil, bldrInfoFinder)
			_, err := hf.Build(tt.hndlrCnfg, tt.instsCnfg, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("got error %v\nwant %v", err, tt.wantError)
			}

		})
	}
}

func TestBuild_Valid(t *testing.T) {
	tests := []struct {
		name string

		instsCnfg []*pb.Instance
		hndlrCnfg *pb.Handler

		tmplRepo     fakeTmplRepo
		hndlrBuilder adapter.HandlerBuilder

		wantCnfgMtdCallInfo map[string]map[string]proto.Message // templateName - > map[instName]InferredType (proto.Message)
		wantBldMtdCnfgParam proto.Message                       // expected adaper-cnfg passed to the HandlerBuilder in handlerBuilder.Build mtd
	}{
		{
			name:         "SingleInstance",
			tmplRepo:     fakeTmplRepo{typeResult: &wrappers.Int32Value{Value: 1}, cnfgMtdCallInfo: make(map[string]map[string]proto.Message)},
			instsCnfg:    []*pb.Instance{{"inst1", "tmpl1", &empty.Empty{}}},
			hndlrCnfg:    &pb.Handler{Name: "h1", Adapter: "a1", Params: &wrappers.Int32Value{Value: 2}},
			hndlrBuilder: fakeHndlrBldr{},

			wantCnfgMtdCallInfo: map[string]map[string]proto.Message{"tmpl1": {"inst1": &wrappers.Int32Value{Value: 1}}},
			wantBldMtdCnfgParam: &wrappers.Int32Value{Value: 2},
		},
		{
			name:         "EmptyInstance",
			tmplRepo:     fakeTmplRepo{cnfgMtdCallInfo: make(map[string]map[string]proto.Message)},
			instsCnfg:    []*pb.Instance{},
			hndlrCnfg:    &pb.Handler{Name: "h1", Adapter: "a1", Params: &wrappers.Int32Value{Value: 2}},
			hndlrBuilder: fakeHndlrBldr{},

			wantCnfgMtdCallInfo: map[string]map[string]proto.Message{},
			wantBldMtdCnfgParam: &wrappers.Int32Value{Value: 2},
		},
		{
			name:     "SingleTmplMultipleInstances",
			tmplRepo: fakeTmplRepo{typeResult: &wrappers.Int32Value{Value: 1}, cnfgMtdCallInfo: make(map[string]map[string]proto.Message)},
			instsCnfg: []*pb.Instance{
				{"inst1", "tmpl1", &empty.Empty{}},
				{"inst2", "tmpl1", &empty.Empty{}},
			},
			hndlrCnfg:    &pb.Handler{Name: "h1", Adapter: "a1", Params: &wrappers.Int32Value{Value: 2}},
			hndlrBuilder: fakeHndlrBldr{},

			wantCnfgMtdCallInfo: map[string]map[string]proto.Message{
				"tmpl1": {
					"inst2": &wrappers.Int32Value{Value: 1},
					"inst1": &wrappers.Int32Value{Value: 1},
				},
			},
			wantBldMtdCnfgParam: &wrappers.Int32Value{Value: 2},
		},
		{
			name:     "DedupeInstances",
			tmplRepo: fakeTmplRepo{typeResult: &wrappers.Int32Value{Value: 1}, cnfgMtdCallInfo: make(map[string]map[string]proto.Message)},
			instsCnfg: []*pb.Instance{
				{"dupe", "tmpl1", &empty.Empty{}},
				{"dupe", "tmpl1", &empty.Empty{}},
				{"inst2", "tmpl1", &empty.Empty{}},
			},
			hndlrCnfg:    &pb.Handler{Name: "h1", Adapter: "a1", Params: &wrappers.Int32Value{Value: 2}},
			hndlrBuilder: fakeHndlrBldr{},

			wantCnfgMtdCallInfo: map[string]map[string]proto.Message{
				"tmpl1": {
					"inst2": &wrappers.Int32Value{Value: 1},
					"dupe":  &wrappers.Int32Value{Value: 1},
				},
			},
			wantBldMtdCnfgParam: &wrappers.Int32Value{Value: 2},
		},
		{
			name:     "MultipleTmplMultipleInstances",
			tmplRepo: fakeTmplRepo{typeResult: &wrappers.Int32Value{Value: 1}, cnfgMtdCallInfo: make(map[string]map[string]proto.Message)},
			instsCnfg: []*pb.Instance{
				{"inst1", "tmpl1", &empty.Empty{}},
				{"inst2", "tmpl1", &empty.Empty{}},
				{"inst3", "tmpl1", &empty.Empty{}},

				{"inst4", "tmpl2", &empty.Empty{}},
				{"inst5", "tmpl2", &empty.Empty{}},
				{"inst6", "tmpl2", &empty.Empty{}},
			},
			hndlrCnfg:    &pb.Handler{Name: "h1", Adapter: "a1", Params: &wrappers.Int32Value{Value: 2}},
			hndlrBuilder: fakeHndlrBldr{},

			wantCnfgMtdCallInfo: map[string]map[string]proto.Message{
				"tmpl1": {
					"inst1": &wrappers.Int32Value{Value: 1},
					"inst2": &wrappers.Int32Value{Value: 1},
					"inst3": &wrappers.Int32Value{Value: 1},
				},
				"tmpl2": {
					"inst4": &wrappers.Int32Value{Value: 1},
					"inst5": &wrappers.Int32Value{Value: 1},
					"inst6": &wrappers.Int32Value{Value: 1},
				},
			},
			wantBldMtdCnfgParam: &wrappers.Int32Value{Value: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			bldrInfoFinder := func(name string) (*handler.Info, bool) {
				return &handler.Info{CreateHandlerBuilder: func() adapter.HandlerBuilder { return tt.hndlrBuilder }}, true
			}

			hf := NewHandlerFactory(tt.tmplRepo, nil, nil, bldrInfoFinder)
			hndlr, err := hf.Build(tt.hndlrCnfg, tt.instsCnfg, nil)
			if err != nil {
				t.Fatalf("got err %v\nwant <nil>", err)
			}
			fHndlr := hndlr.(fakeHndlr)
			if !reflect.DeepEqual(tt.wantCnfgMtdCallInfo, tt.tmplRepo.cnfgMtdCallInfo) {
				t.Errorf("got %v\nwant %v", tt.tmplRepo.cnfgMtdCallInfo, tt.wantCnfgMtdCallInfo)
			}
			if !reflect.DeepEqual(tt.wantBldMtdCnfgParam, fHndlr.createdWithCnfg) {
				t.Errorf("got %v\nwant %v", fHndlr.createdWithCnfg, tt.wantBldMtdCnfgParam)
			}
		})
	}
}
