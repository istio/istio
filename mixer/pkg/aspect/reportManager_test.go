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

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/attribute"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/template"
)

type fakeReportHandler struct{}

func (h *fakeReportHandler) Close() error { return nil }

func TestNewReportManager(t *testing.T) {
	r := template.NewRepository(map[string]template.Info{"foo": {BldrInterfaceName: "fooProcBuilder"}})
	m := NewReportManager(r)
	if !reflect.DeepEqual(m.(*reportManager).repo, r) {
		t.Errorf("m.repo = %v wanted %v", m.(*reportManager).repo, r)
	}
}

func TestReportManager_NewReportExecutor(t *testing.T) {
	handler := &fakeReportHandler{}

	tmplName := "TestReportTemplate"
	instName := "TestReportInstanceName"

	conf := &cpb.Combined{
		Instances: []*cpb.Instance{
			{
				Template: tmplName,
				Name:     instName,
				Params:   &types.Empty{},
			},
		},
	}

	f := FromHandler(handler)
	tmplRepo := template.NewRepository(map[string]template.Info{
		tmplName: {HandlerSupportsTemplate: func(hndlr adapter.Handler) bool { return true }},
	})

	if e, err := NewReportManager(tmplRepo).NewReportExecutor(conf, f, nil, df, tmplName); err != nil {
		t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)) = _, %v; wanted no err", err)
	} else {
		qe := e.(*reportExecutor)
		if !reflect.DeepEqual(qe.hndlr, handler) {
			t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)).hdnlr = %v; wanted %v", qe.hndlr, handler)
		}
		if qe.tmplName != tmplName {
			t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)).tmplName = %v; wanted %v", qe.tmplName, tmplName)
		}
		wantInsts := map[string]proto.Message{instName: conf.Instances[0].Params.(proto.Message)}
		if !reflect.DeepEqual(qe.insts, wantInsts) {
			t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)).insts = %v; wanted %v", qe.insts, wantInsts)
		}
	}
}

func TestReportManager_NewReportExecutorErrors(t *testing.T) {
	tmplName := "TestReportTemplate"

	tests := []struct {
		name          string
		ctr           cpb.Instance
		hndlrSuppTmpl bool
		wantErr       string
	}{
		{
			name:    "NotFoundTemplate",
			wantErr: "template is different",
			ctr: cpb.Instance{
				Template: "NotFoundTemplate",
				Name:     "SomeInstName",
				Params:   &types.Empty{},
			},
			hndlrSuppTmpl: true,
		},
		{
			name:          "BadHandlerInterface",
			wantErr:       "does not implement interface",
			hndlrSuppTmpl: false,
			ctr: cpb.Instance{
				Template: tmplName,
				Name:     "SomeInstName",
				Params:   &types.Empty{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &fakeReportHandler{}
			conf := &cpb.Combined{
				Instances: []*cpb.Instance{
					&tt.ctr,
				},
			}
			tmplRepo := template.NewRepository(
				map[string]template.Info{tmplName: {HandlerSupportsTemplate: func(hndlr adapter.Handler) bool { return tt.hndlrSuppTmpl }}},
			)
			f := FromHandler(handler)
			if _, err := NewReportManager(tmplRepo).NewReportExecutor(conf, f, nil, df, tmplName); !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)) error = %v; wanted err %s", err, tt.wantErr)
			}
		})
	}
}

func TestNewReportExecutor_Execute(t *testing.T) {
	instName := "TestReportInstanceName"
	insts := map[string]proto.Message{
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
			rProc := func(insts map[string]proto.Message, attrs attribute.Bag, mapper expr.Evaluator, handler adapter.Handler) rpc.Status {
				return tt.retStatus
			}

			e := &reportExecutor{"TestReportTemplate", rProc, nil, insts}
			s := e.Execute(nil, nil)

			if !reflect.DeepEqual(s, tt.retStatus) {
				t.Fatalf("reportExecutor.Executor(..) = %v; wanted %v", s, tt.retStatus)
			}
		})
	}
}
