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
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	adptConfig "istio.io/mixer/pkg/adapter/config"
	"istio.io/mixer/pkg/attribute"
	cfgpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/template"
)

type fakeQuotaHandler struct{}

func (h *fakeQuotaHandler) Close() error { return nil }

func TestNewQuotaManager(t *testing.T) {
	r := template.NewRepository(map[string]template.Info{"foo": {BldrName: "fooProcBuilder"}})
	m := NewQuotaManager(r)
	if !reflect.DeepEqual(m.(*quotaManager).repo, r) {
		t.Errorf("m.repo = %v wanted %v", m.(*quotaManager).repo, r)
	}
}

func TestQuotaManager_NewQuotaExecutor(t *testing.T) {
	handler := &fakeQuotaHandler{}

	tmplName := "TestQuotaTemplate"
	instName := "TestQuotaInstanceName"

	conf := &cfgpb.Combined{
		Instances: []*cfgpb.Instance{
			{
				Template: tmplName,
				Name:     instName,
				Params:   &types.Empty{},
			},
		},
	}

	f := FromHandler(handler)
	tmplRepo := template.NewRepository(map[string]template.Info{
		tmplName: {HandlerSupportsTemplate: func(hndlr adptConfig.Handler) bool { return true }},
	})

	if e, err := NewQuotaManager(tmplRepo).NewQuotaExecutor(conf, f, nil, df, tmplName); err != nil {
		t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)) = _, %v; wanted no err", err)
	} else {
		qe := e.(*quotaExecutor)
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

func TestQuotaManager_NewQuotaExecutorErrors(t *testing.T) {
	tmplName := "TestQuotaTemplate"

	tests := []struct {
		name          string
		ctr           cfgpb.Instance
		hndlrSuppTmpl bool
		wantErr       string
	}{
		{
			name:    "NotFoundTemplate",
			wantErr: "template is different",
			ctr: cfgpb.Instance{
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
			ctr: cfgpb.Instance{
				Template: tmplName,
				Name:     "SomeInstName",
				Params:   &types.Empty{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &fakeQuotaHandler{}
			conf := &cfgpb.Combined{
				Instances: []*cfgpb.Instance{
					&tt.ctr,
				},
			}
			tmplRepo := template.NewRepository(
				map[string]template.Info{tmplName: {HandlerSupportsTemplate: func(hndlr adptConfig.Handler) bool { return tt.hndlrSuppTmpl }}},
			)
			f := FromHandler(handler)
			if _, err := NewQuotaManager(tmplRepo).NewQuotaExecutor(conf, f, nil, df, tmplName); !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewExecutor(conf, builder, test.NewEnv(t)) error = %v; wanted err %s", err, tt.wantErr)
			}
		})
	}
}

func TestNewQuotaExecutor_Execute(t *testing.T) {
	instName := "TestQuotaInstanceName"
	insts := map[string]proto.Message{
		instName: &types.Empty{},
	}

	tests := []struct {
		name      string
		retQR     adapter.QuotaResult
		retStatus rpc.Status
		quotaName string
		wantErr   string
	}{
		{
			name:      "Valid",
			quotaName: instName,
			retQR:     adapter.QuotaResult{Amount: 100},
			retStatus: status.OK,
		},
		{
			name:      "BadQuotaName",
			quotaName: "UnknownQuotaName",
			wantErr:   "unknown quota ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qProc := func(quotaName string, cnstr proto.Message, attrs attribute.Bag, mapper expr.Evaluator, handler adptConfig.Handler,
				qma adapter.QuotaRequestArgs) (rpc.Status, adptConfig.CacheabilityInfo, adapter.QuotaResult) {
				return tt.retStatus, adptConfig.CacheabilityInfo{}, tt.retQR
			}
			e := &quotaExecutor{"TestQuotaTemplate", qProc, nil, insts}
			s, qmr := e.Execute(nil, nil, &QuotaMethodArgs{Quota: tt.quotaName})

			if tt.wantErr == "" {
				if !reflect.DeepEqual(s, tt.retStatus) || !reflect.DeepEqual(*qmr, QuotaMethodResp(tt.retQR)) {
					t.Fatalf("quotaExecutor.Executor(..) = %v, %v; wanted %v, %v", s, *qmr, tt.retStatus, QuotaMethodResp(tt.retQR))
				}
			} else {
				if !strings.Contains(s.Message, tt.wantErr) {
					t.Fatalf("quotaExecutor.Execute(..) error = %s; wanted err %s", s.Message, tt.wantErr)
				}
			}
		})
	}
}
