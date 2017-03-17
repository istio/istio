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

package aspect

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	ptypes "github.com/gogo/protobuf/types"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	atest "istio.io/mixer/pkg/adapter/test"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

type fakeQuotaAspect struct {
	adapter.Aspect
	closed bool
	body   func(adapter.QuotaArgs) (adapter.QuotaResult, error)
}

func (a *fakeQuotaAspect) Close() error {
	a.closed = true
	return nil
}

func (a fakeQuotaAspect) Alloc(qa adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return a.body(qa)
}

func (a fakeQuotaAspect) AllocBestEffort(qa adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return a.body(qa)
}

func (a fakeQuotaAspect) ReleaseBestEffort(adapter.QuotaArgs) (int64, error) {
	return 0, nil
}

type fakeQuotaBuilder struct {
	adapter.Builder
	name string

	body func() (adapter.QuotasAspect, error)
}

func (b *fakeQuotaBuilder) Name() string {
	return b.name
}

func (b *fakeQuotaBuilder) NewQuotasAspect(env adapter.Env, config adapter.AspectConfig,
	quotas map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return b.body()
}

func TestNewQuotasManager(t *testing.T) {
	m := newQuotasManager()
	if m.Kind() != QuotasKind {
		t.Errorf("m.Kind() = %s wanted %s", m.Kind(), QuotasKind)
	}
	if err := m.ValidateConfig(m.DefaultConfig()); err != nil {
		t.Errorf("m.ValidateConfig(m.DefaultConfig()) = %v; wanted no err", err)
	}
}

func newQuotaConfig(desc string, labels map[string]string) *config.Combined {
	return &config.Combined{
		Aspect: &pb.Aspect{
			Params: &aconfig.QuotasParams{
				Quotas: []*aconfig.QuotasParams_Quota{
					{
						DescriptorName: desc,
						Labels:         labels,
					},
				},
			},
		},

		// the params we use here don't matter because we're faking the aspect
		Builder: &pb.Adapter{Params: &aconfig.QuotasParams{}},
	}
}

func TestQuotasManager_NewAspect(t *testing.T) {
	builder := &fakeQuotaBuilder{name: "test", body: func() (adapter.QuotasAspect, error) {
		return &fakeQuotaAspect{}, nil
	}}

	conf := newQuotaConfig("RequestCount", map[string]string{"source": "", "target": ""})
	if _, err := newQuotasManager().NewAspect(conf, builder, atest.NewEnv(t)); err != nil {
		t.Errorf("NewAspect(conf, builder, test.NewEnv(t)) = _, %v; wanted no err", err)
	}

	conf = newQuotaConfig("FOOBAR", map[string]string{})
	if _, err := newQuotasManager().NewAspect(conf, builder, atest.NewEnv(t)); err != nil {
		t.Errorf("NewAspect(conf, builder, test.NewEnv(t)) = _, %v; wanted no err", err)
	}
}

func TestQuotasManager_NewAspect_PropagatesError(t *testing.T) {
	conf := &config.Combined{
		Aspect: &pb.Aspect{Params: &aconfig.QuotasParams{}},
		// the params we use here don't matter because we're faking the aspect
		Builder: &pb.Adapter{Params: &aconfig.QuotasParams{}},
	}
	errString := "expected"
	builder := &fakeQuotaBuilder{
		body: func() (adapter.QuotasAspect, error) {
			return nil, errors.New(errString)
		}}
	_, err := newQuotasManager().NewAspect(conf, builder, atest.NewEnv(t))
	if err == nil {
		t.Error("newQuotasManager().NewAspect(conf, builder, test.NewEnv(t)) = _, nil; wanted err")
	}
	if !strings.Contains(err.Error(), errString) {
		t.Errorf("NewAspect(conf, builder, test.NewEnv(t)) = _, %v; wanted err %s", err, errString)
	}
}

func TestQuotaWrapper_Execute(t *testing.T) {
	goodEval := test.NewFakeEval(func(exp string, _ attribute.Bag) (interface{}, error) {
		switch exp {
		case "value":
			return 1, nil
		case "source":
			return "me", nil
		case "target":
			return "you", nil
		case "service":
			return "echo", nil
		default:
			return nil, fmt.Errorf("default case for exp = %s", exp)
		}
	})
	errEval := test.NewFakeEval(func(_ string, _ attribute.Bag) (interface{}, error) {
		return nil, errors.New("expected")
	})
	labelErrEval := test.NewFakeEval(func(exp string, _ attribute.Bag) (interface{}, error) {
		switch exp {
		case "value":
			return 1, nil
		default:
			return nil, errors.New("expected")
		}
	})

	goodMd := map[string]*quotaInfo{
		"request_count": {
			definition: &adapter.QuotaDefinition{Name: "request_count"},
			labels: map[string]string{
				"source":  "source",
				"target":  "target",
				"service": "service",
			},
		},
	}

	type o struct {
		amount int64
		labels []string
	}
	cases := []struct {
		mdin        map[string]*quotaInfo
		allocAmount int64
		allocErr    error
		bestEffort  bool
		eval        expr.Evaluator
		out         map[string]o
		errString   string
	}{
		{make(map[string]*quotaInfo), 1, nil, false, test.NewIDEval(), make(map[string]o), ""},
		{goodMd, 1, nil, false, errEval, make(map[string]o), "expected"},
		{goodMd, 1, nil, false, labelErrEval, make(map[string]o), "expected"},
		{goodMd, 1, nil, false, goodEval, map[string]o{"request_count": {1, []string{"source", "target"}}}, ""},
		{goodMd, 0, errors.New("alloc-forced-error"), false, goodEval,
			map[string]o{"request_count": {1, []string{"source", "target"}}}, "alloc-forced-error"},
		{goodMd, 1, nil, true, goodEval, map[string]o{"request_count": {1, []string{"source", "target"}}}, ""},
		{goodMd, 0, nil, false, goodEval, map[string]o{"request_count": {1, []string{"source", "target"}}}, ""},
	}
	for idx, c := range cases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			var receivedArgs adapter.QuotaArgs
			wrapper := &quotasWrapper{
				aspect: &fakeQuotaAspect{body: func(qa adapter.QuotaArgs) (adapter.QuotaResult, error) {
					receivedArgs = qa
					return adapter.QuotaResult{Amount: c.allocAmount, Expiration: time.Duration(0)}, c.allocErr
				}},
				metadata: c.mdin,
			}
			out := wrapper.Execute(test.NewBag(), c.eval, &QuotaMethodArgs{
				Quota:      "request_count",
				Amount:     1,
				BestEffort: c.bestEffort,
			})

			errString := out.Message()
			if !strings.Contains(errString, c.errString) {
				t.Errorf("wrapper.Execute(&fakeBag{}, eval) = _, %v; wanted error containing %s", out.Message(), c.errString)
			}

			if out.IsOK() {
				o, found := c.out[receivedArgs.Definition.Name]
				if !found {
					t.Errorf("Got unexpected args %v, wanted only %v", receivedArgs, c.out)
				}
				if receivedArgs.QuotaAmount != o.amount {
					t.Errorf("receivedArgs.QuotaAmount = %v; wanted %v", receivedArgs.QuotaAmount, o.amount)
				}
				for _, l := range o.labels {
					if _, found := receivedArgs.Labels[l]; !found {
						t.Errorf("value.Labels = %v; wanted label named %s", receivedArgs.Labels, l)
					}
				}
			}
		})
	}
}

func TestQuotasWrapper_Close(t *testing.T) {
	inner := &fakeQuotaAspect{closed: false}
	wrapper := &quotasWrapper{aspect: inner}
	if err := wrapper.Close(); err != nil {
		t.Errorf("wrapper.Close() = %v; wanted no err", err)
	}
	if !inner.closed {
		t.Error("quotasWrapper.Close() didn't close the aspect inside")
	}
}

func TestQuotas_DescToDef(t *testing.T) {
	cases := []struct {
		in        *dpb.QuotaDescriptor
		out       *adapter.QuotaDefinition
		errString string
	}{
		{
			&dpb.QuotaDescriptor{
				Name:   "bad label",
				Labels: []*dpb.LabelDescriptor{{ValueType: dpb.VALUE_TYPE_UNSPECIFIED}},
			},
			nil,
			"VALUE_TYPE_UNSPECIFIED",
		},
		{
			&dpb.QuotaDescriptor{
				Name:        "NAME",
				DisplayName: "DISPLAYNAME",
				Description: "DESCRIPTION",
				MaxAmount:   123,
				Labels:      []*dpb.LabelDescriptor{{Name: "string", ValueType: dpb.STRING}},
				Expiration:  ptypes.DurationProto(time.Duration(42) * time.Second),
			},
			&adapter.QuotaDefinition{
				Name:        "NAME",
				DisplayName: "DISPLAYNAME",
				Description: "DESCRIPTION",
				MaxAmount:   123,
				Expiration:  time.Duration(42) * time.Second,
				Labels:      map[string]adapter.LabelType{"string": adapter.String},
			},
			"",
		},
	}
	for idx, c := range cases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			result, err := quotaDefinitionFromProto(c.in)

			errString := ""
			if err != nil {
				errString = err.Error()
			}
			if !strings.Contains(errString, c.errString) {
				t.Errorf("quotaDefinitionFromProto(%v) = _, %v; wanted err containing %s", c.in, err, c.errString)
			}
			if !reflect.DeepEqual(result, c.out) {
				t.Errorf("quotaDefinitionFromProto(%v) = %v, %v; wanted %v", c.in, result, err, c.out)
			}
		})
	}
}

func TestQuotas_Find(t *testing.T) {
	cases := []struct {
		in   []*aconfig.QuotasParams_Quota
		find string
		out  bool
	}{
		{[]*aconfig.QuotasParams_Quota{}, "", false},
		{[]*aconfig.QuotasParams_Quota{{DescriptorName: "foo"}}, "foo", true},
		{[]*aconfig.QuotasParams_Quota{{DescriptorName: "bar"}}, "foo", false},
	}
	for _, c := range cases {
		t.Run(c.find, func(t *testing.T) {
			if q := findQuota(c.in, c.find); (q != nil) != c.out {
				t.Errorf("find(%v, %s) = _, %t; wanted %t", c.in, c.find, q != nil, c.out)
			}
		})
	}
}
