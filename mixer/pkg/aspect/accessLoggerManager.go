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

package aspect

import (
	"bytes"
	"fmt"
	"text/template"

	"google.golang.org/genproto/googleapis/rpc/code"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

type (
	accessLoggerManager struct{}

	accessLoggerWrapper struct {
		logName   string
		inputs    map[string]string // map from param to expr
		aspect    adapter.AccessLoggerAspect
		attrNames []string
		template  *template.Template
	}
)

const (
	// TODO: revisit when well-known attributes are defined.
	commonLogFormat = `{{or (.source_ip) "-"}} - {{or (.source_user) "-"}} ` +
		`[{{.timestamp.Format "02/Jan/2006:15:04:05 -0700"}}] "{{or (.api_method) "-"}} ` +
		`{{or (.api_resource) "-"}} {{or (.protocol) "-"}}" {{or (.status_code) "-"}} {{or (.response_size) "-"}}`
	// TODO: revisit when well-known attributes are defined.
	combinedLogFormat = commonLogFormat + ` "{{or (.referer) "-"}}" "{{or (.user_agent) "-"}}"`
)

var (
	// TODO: revisit when well-known attributes are defined
	commonLogAttributes = []string{"source_ip", "source_user", "timestamp", "api_method", "api_resource", "protocol", "status_code", "response_size"}
	// TODO: revisit when well-known attributes are defined
	combinedLogAttributes = append(commonLogAttributes, "referer", "user_agent")
)

// NewAccessLoggerManager returns an instance of the accessLogger aspect manager.
func NewAccessLoggerManager() Manager {
	return accessLoggerManager{}
}

func (m accessLoggerManager) NewAspect(c *CombinedConfig, a adapter.Builder, env adapter.Env) (Wrapper, error) {
	// Handle aspect config to get log name and log entry descriptors.

	// WARNING: this will cause an override of any `attributes` spec in the
	// supplied c.Aspect.Params. This will need to be tested and reviewed
	// when the config path switches away from structToProto merging.
	aspectCfg := m.DefaultConfig()
	if c.Aspect.Params != nil {
		if err := structToProto(c.Aspect.Params, aspectCfg); err != nil {
			return nil, fmt.Errorf("could not parse aspect config: %v", err)
		}
	}

	// cast to adapter.AccessLoggerBuilder from adapter.Builder
	logBuilder, ok := a.(adapter.AccessLoggerBuilder)
	if !ok {
		return nil, fmt.Errorf("adapter of incorrect type. Expected adapter.AccessLoggerBuilder got %#v %T", a, a)
	}

	// Handle adapter config
	cpb := logBuilder.DefaultConfig()
	if c.Builder.Params != nil {
		if err := structToProto(c.Builder.Params, cpb); err != nil {
			return nil, fmt.Errorf("could not parse adapter config: %v", err)
		}
	}

	aspectImpl, err := logBuilder.NewAccessLogger(env, cpb)
	if err != nil {
		return nil, err
	}

	logCfg := aspectCfg.(*config.AccessLoggerParams)
	logName := logCfg.LogName
	logFormat := logCfg.LogFormat
	var attrNames []string
	var templateStr string
	switch logFormat {
	case config.AccessLoggerParams_COMMON:
		attrNames = commonLogAttributes
		templateStr = commonLogFormat
	case config.AccessLoggerParams_COMBINED:
		attrNames = combinedLogAttributes
		templateStr = combinedLogFormat
	case config.AccessLoggerParams_CUSTOM:
		fallthrough
	default:
		templateStr = logCfg.CustomLogTemplate
		attrNames = logCfg.Attributes
	}

	var inputs map[string]string
	if c.Aspect != nil && c.Aspect.Inputs != nil {
		inputs = c.Aspect.Inputs
	}

	tmpl, err := template.New("accessLoggerTemplate").Parse(templateStr)
	if err != nil {
		// should never happen, as this should fail ValidateConfig()
		return nil, err
	}

	return &accessLoggerWrapper{
		logName,
		inputs,
		aspectImpl,
		attrNames,
		tmpl,
	}, nil
}

func (accessLoggerManager) Kind() string { return "istio/accessLogger" }
func (accessLoggerManager) DefaultConfig() adapter.AspectConfig {
	return &config.AccessLoggerParams{
		LogName:   "access_log",
		LogFormat: config.AccessLoggerParams_COMMON,
		// WARNING: we cannot set default attributes here, based on
		// the params -> proto merge logic. These will override
		// all other values. This should be mitigated by the move
		// away from structpb-based config merging.
	}
}

func (accessLoggerManager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	cfg := c.(*config.AccessLoggerParams)
	if cfg.LogFormat != config.AccessLoggerParams_CUSTOM {
		return nil
	}
	tmplStr := cfg.CustomLogTemplate
	if _, err := template.New("test").Parse(tmplStr); err != nil {
		return ce.Appendf("CustomLogTemplate", "could not parse template string: %v", err)
	}
	return nil
}

func (e *accessLoggerWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error) {
	// TODO: would be nice if we could use a mutable.Bag here and could pass it around
	// labels holds the generated attributes from mapper
	labels := make(map[string]interface{})
	for attr, expr := range e.inputs {
		if val, err := mapper.Eval(expr, attrs); err == nil {
			labels[attr] = val
		}
		// TODO: throw error on failed mapping?
	}

	entry := adapter.AccessLogEntry{
		LogName: e.logName,
		Labels:  make(map[string]interface{}),
	}

	for _, a := range e.attrNames {
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

	buf := new(bytes.Buffer)
	if err := e.template.Execute(buf, entry.Labels); err != nil {
		return nil, err
	}
	entry.Log = buf.String()
	if err := e.aspect.LogAccess([]adapter.AccessLogEntry{entry}); err != nil {
		return nil, err
	}
	return &Output{Code: code.Code_OK}, nil
}
