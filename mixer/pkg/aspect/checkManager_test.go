// Copyright 2017 Istio Authors.
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

package aspect

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	rpc "github.com/googleapis/googleapis/google/rpc"

	adptConfig "istio.io/mixer/pkg/adapter/config"
	"istio.io/mixer/pkg/attribute"
	cfgpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/template"
)

type fakeCheckHandler struct{}

func (h *fakeCheckHandler) Close() error { return nil }

func TestNewCheckManager(t *testing.T) {
	r := template.NewRepository(map[string]template.Info{"foo": {BldrName: "fooProcBuilder"}})
	m := NewCheckManager(r)
	if !reflect.DeepEqual(m.(*checkManager).repo, r) {
		t.Errorf("m.repo = %v wanted %v", m.(*checkManager).repo, r)
	}
}

func TestCheckManager_NewCheckExecutor(t *testing.T) {
	handler := &fakeCheckHandler{}

	tmplName := "TestCheckTemplate"
	instName := "TestCheckInstanceName"

	conf := &cfgpb.Combined{
		Constructors: []*cfgpb.Constructor{
			{
				Template:     tmplName,
				InstanceName: instName,
				Params:       &types.Empty{},
			},
		},
	}

	f := FromHandler(handler)
	tmplRepo := template.NewRepository(map[string]template.Info{
		tmplName: {HandlerSupportsTemplate: func(hndlr adptConfig.Handler) bool { return true }},
	})

	if e, err := NewCheckManager(tmplRepo).NewCheckExecutor(conf, f, nil, df, tmplName); err != nil {
		t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)) = _, %v; wanted no err", err)
	} else {
		qe := e.(*checkExecutor)
		if !reflect.DeepEqual(qe.hndlr, handler) {
			t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)).hdnlr = %v; wanted %v", qe.hndlr, handler)
		}
		if qe.tmplName != tmplName {
			t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)).tmplName = %v; wanted %v", qe.tmplName, tmplName)
		}
		wantCtrs := map[string]proto.Message{instName: conf.Constructors[0].Params.(proto.Message)}
		if !reflect.DeepEqual(qe.ctrs, wantCtrs) {
			t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)).ctrs = %v; wanted %v", qe.ctrs, wantCtrs)
		}
	}
}

func TestCheckManager_NewCheckExecutorErrors(t *testing.T) {
	tmplName := "TestCheckTemplate"

	tests := []struct {
		name          string
		ctr           cfgpb.Constructor
		hndlrSuppTmpl bool
		wantErr       string
	}{
		{
			name:    "NotFoundTemplate",
			wantErr: "template is different",
			ctr: cfgpb.Constructor{
				Template:     "NotFoundTemplate",
				InstanceName: "SomeInstName",
				Params:       &types.Empty{},
			},
			hndlrSuppTmpl: true,
		},
		{
			name:          "BadHandlerInterface",
			wantErr:       "does not implement interface",
			hndlrSuppTmpl: false,
			ctr: cfgpb.Constructor{
				Template:     tmplName,
				InstanceName: "SomeInstName",
				Params:       &types.Empty{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &fakeCheckHandler{}
			conf := &cfgpb.Combined{
				Constructors: []*cfgpb.Constructor{
					&tt.ctr,
				},
			}
			tmplRepo := template.NewRepository(
				map[string]template.Info{tmplName: {HandlerSupportsTemplate: func(hndlr adptConfig.Handler) bool { return tt.hndlrSuppTmpl }}},
			)
			f := FromHandler(handler)
			if _, err := NewCheckManager(tmplRepo).NewCheckExecutor(conf, f, nil, df, tmplName); !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)) error = %v; wanted err %s", err, tt.wantErr)
			}
		})
	}
}

func TestNewCheckExecutor_Execute(t *testing.T) {
	instName := "TestCheckInstanceName"
	ctrs := map[string]proto.Message{
		instName: &types.Empty{},
	}

	tests := []struct {
		name      string
		retStatus rpc.Status
	}{
		{
			name:      "Valid",
			retStatus: status.OK,
		},
		{
			name:      "status.Fail",
			retStatus: status.WithError(fmt.Errorf("testerror")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cProc := func(ctrs map[string]proto.Message, attrs attribute.Bag, mapper expr.Evaluator,
				handler adptConfig.Handler) (rpc.Status, adptConfig.CacheabilityInfo) {
				return tt.retStatus, adptConfig.CacheabilityInfo{}
			}
			e := &checkExecutor{"TestCheckTemplate", cProc, nil, ctrs}
			s := e.Execute(nil, nil)

			if !reflect.DeepEqual(s, tt.retStatus) {
				t.Fatalf("checkExecutor.Executor(..) = %v; wanted %v", s, tt.retStatus)
			}
		})
	}
}
