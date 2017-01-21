// Copyright 2017 Google Inc.
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

package logger

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/genproto/googleapis/rpc/code"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/logger"
	"istio.io/mixer/pkg/aspectsupport"
	"istio.io/mixer/pkg/aspectsupport/logger/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"

	dpb "istio.io/api/mixer/v1/config/descriptor"
)

type (
	manager struct{}

	executor struct {
		logName            string
		descriptors        []dpb.LogEntryDescriptor // describe entries to gen
		inputs             map[string]string        // map from param to expr
		severityAttribute  string
		timestampAttribute string
		timestampFmt       string
		aspect             logger.Aspect
		defaultTimeFn      func() time.Time
	}
)

// NewManager returns an aspect manager for the logger aspect.
func NewManager() aspectsupport.Manager {
	return &manager{}
}

func (m *manager) NewAspect(c *aspectsupport.CombinedConfig, a adapter.Adapter, env adapter.Env) (aspectsupport.AspectWrapper, error) {
	// Handle aspect config to get log name and log entry descriptors.
	aspectCfg := m.DefaultConfig()
	if c.Aspect.Params != nil {
		if err := structToProto(c.Aspect.Params, aspectCfg); err != nil {
			return nil, fmt.Errorf("could not parse aspect config: %v", err)
		}
	}

	logCfg := aspectCfg.(*config.Params)
	logName := logCfg.LogName
	severityAttr := logCfg.SeverityAttribute
	timestampAttr := logCfg.TimestampAttribute
	timestampFmt := logCfg.TimestampFormat
	// TODO: look up actual descriptors by name and build an array

	// cast to logger.Adapter from adapter.Adapter
	logAdapter, ok := a.(logger.Adapter)
	if !ok {
		return nil, fmt.Errorf("adapter of incorrect type. Expected logger.Adapter got %#v %T", a, a)
	}

	// Handle adapter config
	cpb := logAdapter.DefaultConfig()
	if c.Adapter.Params != nil {
		if err := structToProto(c.Adapter.Params, cpb); err != nil {
			return nil, fmt.Errorf("could not parse adapter config: %v", err)
		}
	}

	aspectImpl, err := logAdapter.NewLogger(env, cpb)
	if err != nil {
		return nil, err
	}

	var inputs map[string]string
	if c.Aspect != nil && c.Aspect.Inputs != nil {
		inputs = c.Aspect.Inputs
	}

	return &executor{
		logName,
		[]dpb.LogEntryDescriptor{},
		inputs,
		severityAttr,
		timestampAttr,
		timestampFmt,
		aspectImpl,
		time.Now,
	}, nil
}

func (*manager) Kind() string { return "istio/logger" }
func (*manager) DefaultConfig() adapter.Config {
	return &config.Params{LogName: "istio_log", TimestampFormat: time.RFC3339}
}

// TODO: validation of timestamp format
func (*manager) ValidateConfig(c adapter.Config) (ce *adapter.ConfigErrors) { return nil }

func (e *executor) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*aspectsupport.Output, error) {
	var entries []logger.Entry

	// TODO: would be nice if we could use a mutable.Bag here and could pass it around
	// labels holds the generated attributes from mapper
	labels := make(map[string]interface{})
	for attr, expr := range e.inputs {
		if val, err := mapper.Eval(expr, attrs); err == nil {
			labels[attr] = val
		}
	}

	for _, d := range e.descriptors {
		entry := logger.Entry{
			LogName:   e.logName,
			Labels:    make(map[string]interface{}),
			Severity:  severityVal(e.severityAttribute, attrs, labels),
			Timestamp: timeVal(e.timestampAttribute, attrs, labels, e.defaultTimeFn()).Format(e.timestampFmt),
		}

		payloadStr := stringVal(d.PayloadAttribute, attrs, labels, "")
		switch d.PayloadFormat {
		case dpb.LogEntryDescriptor_TEXT:
			entry.TextPayload = payloadStr
		case dpb.LogEntryDescriptor_JSON:
			err := json.Unmarshal([]byte(payloadStr), &entry.StructPayload)
			if err != nil {
				return nil, fmt.Errorf("could not unmarshal json payload: %v", err)
			}
		}

		for _, a := range d.Attributes {
			if a == d.PayloadAttribute {
				continue
			}
			if val, ok := labels[a]; ok {
				entry.Labels[a] = val
				continue
			}
			if val, found := attribute.Value(attrs, a); found {
				entry.Labels[a] = val
			}

			// TODO: do we want to error for attributes that cannot
			// be found?
		}

		entries = append(entries, entry)
	}

	if len(entries) > 0 {
		if err := e.aspect.Log(entries); err != nil {
			return nil, err
		}
	}
	return &aspectsupport.Output{Code: code.Code_OK}, nil
}

type attrBagFn func(bag attribute.Bag, name string) (interface{}, bool)

var (
	strFn  = func(bag attribute.Bag, name string) (interface{}, bool) { return bag.String(name) }
	timeFn = func(bag attribute.Bag, name string) (interface{}, bool) { return bag.Time(name) }
)

func stringVal(attrName string, attrs attribute.Bag, labels map[string]interface{}, dfault string) string {
	if v, ok := value(attrName, attrs, strFn, labels).(string); ok {
		return v
	}
	return dfault
}

func severityVal(attrName string, attrs attribute.Bag, labels map[string]interface{}) logger.Severity {
	if name, ok := value(attrName, attrs, strFn, labels).(string); ok {
		if s, found := logger.SeverityByName(name); found {
			return s
		}
	}
	return logger.Default
}

func timeVal(attrName string, attrs attribute.Bag, labels map[string]interface{}, dfault time.Time) time.Time {
	if v, ok := value(attrName, attrs, timeFn, labels).(time.Time); ok {
		return v
	}
	return dfault
}

func value(attrName string, attrBag attribute.Bag, fn attrBagFn, labels map[string]interface{}) interface{} {
	if attrName == "" {
		return nil
	}

	// check generated labels first, then attributes
	if v, ok := labels[attrName]; ok {
		return v
	}

	if v, ok := fn(attrBag, attrName); ok {
		return v
	}

	// TODO: errors needed here? As of now, this causes default vals to be returned
	return nil
}

func structToProto(in *structpb.Struct, out proto.Message) error {
	mm := &jsonpb.Marshaler{}
	str, err := mm.MarshalToString(in)
	if err != nil {
		return fmt.Errorf("failed to marshal to string: %v", err)
	}
	return jsonpb.UnmarshalString(str, out)
}
