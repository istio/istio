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
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/genproto/googleapis/rpc/code"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
)

type (
	applicationLogsManager struct{}

	applicationLogsWrapper struct {
		logName            string
		descriptors        []dpb.LogEntryDescriptor // describe entries to gen
		inputs             map[string]string        // map from param to expr
		severityAttribute  string
		timestampAttribute string
		timestampFmt       string
		aspect             adapter.ApplicationLogsAspect
		defaultTimeFn      func() time.Time
	}
)

var (
	defaultLog = dpb.LogEntryDescriptor{
		Name:             "default",
		DisplayName:      "Default Log Entry",
		Description:      "Placeholder log descriptor",
		PayloadAttribute: "logMessage",
		Attributes: []string{
			"serviceName",
			"peerId",
			"operationId",
			"operationName",
			"apiKey",
			"url",
			"location",
			"apiName",
			"apiVersion",
			"apiMethod",
			"requestSize",
			"responseSize",
			"responseTime",
			"originIp",
			"originHost",
		},
	}
)

// NewApplicationLogsManager returns a manager for the application logs aspect.
func NewApplicationLogsManager() Manager {
	return applicationLogsManager{}
}

func (applicationLogsManager) NewAspect(c *config.Combined, a adapter.Builder, env adapter.Env) (Wrapper, error) {
	aspect, err := a.(adapter.ApplicationLogsBuilder).NewApplicationLogsAspect(env, c.Builder.Params.(adapter.AspectConfig))
	if err != nil {
		return nil, err
	}

	// TODO: look up actual descriptors by name and build an array
	logCfg := c.Aspect.Params.(*aconfig.ApplicationLogsParams)

	return &applicationLogsWrapper{
		logCfg.LogName,
		[]dpb.LogEntryDescriptor{defaultLog},
		c.Aspect.GetInputs(),
		logCfg.SeverityAttribute,
		logCfg.TimestampAttribute,
		logCfg.TimestampFormat,
		aspect,
		time.Now,
	}, nil
}

func (applicationLogsManager) Kind() Kind { return ApplicationLogsKind }
func (applicationLogsManager) DefaultConfig() adapter.AspectConfig {
	return &aconfig.ApplicationLogsParams{LogName: "istio_log", TimestampFormat: time.RFC3339}
}

// TODO: validation of timestamp format
func (applicationLogsManager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	return nil
}

func (e *applicationLogsWrapper) Close() error { return e.aspect.Close() }

func (e *applicationLogsWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator, ma APIMethodArgs) (*Output, error) {
	var entries []adapter.LogEntry

	// TODO: would be nice if we could use a mutable.Bag here and could pass it around
	// labels holds the generated attributes from mapper
	labels := make(map[string]interface{})
	for attr, exp := range e.inputs {
		if val, err := mapper.Eval(exp, attrs); err == nil {
			labels[attr] = val
		}
	}

	for _, d := range e.descriptors {
		entry := adapter.LogEntry{
			LogName:   e.logName,
			Labels:    make(map[string]interface{}),
			Severity:  severityVal(e.severityAttribute, attrs, labels),
			Timestamp: timeVal(e.timestampAttribute, attrs, labels, e.defaultTimeFn()).Format(e.timestampFmt),
		}

		payloadStr := stringVal(d.PayloadAttribute, attrs, labels, "")
		switch d.PayloadFormat {
		case dpb.TEXT:
			entry.TextPayload = payloadStr
		case dpb.JSON:
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
	return &Output{Code: code.Code_OK}, nil
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

func severityVal(attrName string, attrs attribute.Bag, labels map[string]interface{}) adapter.Severity {
	if name, ok := value(attrName, attrs, strFn, labels).(string); ok {
		if s, found := adapter.SeverityByName(name); found {
			return s
		}
	}
	return adapter.Default
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
