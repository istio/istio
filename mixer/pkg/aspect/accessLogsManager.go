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
	"fmt"
	"text/template"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

type (
	accessLogsManager struct{}

	accessLogsWrapper struct {
		name          string
		aspect        adapter.AccessLogsAspect
		labels        map[string]string // label name -> expression
		template      *template.Template
		templateExprs map[string]string // template variable -> expression
	}
)

const (
	// TODO: revisit when well-known attributes are defined.
	commonLogFormat = `{{or (.originIp) "-"}} - {{or (.source_user) "-"}} ` +
		`[{{or (.timestamp.Format "02/Jan/2006:15:04:05 -0700") "-"}}] "{{or (.method) "-"}} ` +
		`{{or (.url) "-"}} {{or (.protocol) "-"}}" {{or (.responseCode) "-"}} {{or (.responseSize) "-"}}`
	// TODO: revisit when well-known attributes are defined.
	combinedLogFormat = commonLogFormat + ` "{{or (.referer) "-"}}" "{{or (.user_agent) "-"}}"`
)

// newAccessLogsManager returns a manager for the access logs aspect.
func newAccessLogsManager() Manager {
	return accessLogsManager{}
}

func (m accessLogsManager) NewAspect(c *cpb.Combined, a adapter.Builder, env adapter.Env, df descriptor.Finder) (Wrapper, error) {
	cfg := c.Aspect.Params.(*aconfig.AccessLogsParams)

	var templateStr string
	switch cfg.Log.LogFormat {
	case aconfig.COMMON:
		templateStr = commonLogFormat
	case aconfig.COMBINED:
		templateStr = combinedLogFormat
	default:
		// TODO: we should never fall into this case because of validation; should we panic?
		templateStr = ""
	}

	// TODO: when users can provide us with descriptors, this error can be removed due to validation
	tmpl, err := template.New("accessLogsTemplate").Parse(templateStr)
	if err != nil {
		return nil, fmt.Errorf("log %s failed to parse template '%s' with err: %s", cfg.LogName, templateStr, err)
	}

	asp, err := a.(adapter.AccessLogsBuilder).NewAccessLogsAspect(env, c.Builder.Params.(adapter.Config))
	if err != nil {
		return nil, fmt.Errorf("failed to create aspect for log %s with err: %s", cfg.LogName, err)
	}

	return &accessLogsWrapper{
		name:          cfg.LogName,
		aspect:        asp,
		labels:        cfg.Log.Labels,
		template:      tmpl,
		templateExprs: cfg.Log.TemplateExpressions,
	}, nil
}

func (accessLogsManager) Kind() Kind { return AccessLogsKind }
func (accessLogsManager) DefaultConfig() config.AspectParams {
	return &aconfig.AccessLogsParams{
		LogName: "access_log",
		Log: &aconfig.AccessLogsParams_AccessLog{
			LogFormat: aconfig.COMMON,
		},
	}
}

func (accessLogsManager) ValidateConfig(c config.AspectParams, _ expr.Validator, _ descriptor.Finder) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.AccessLogsParams)
	if cfg.Log == nil {
		ce = ce.Appendf("Log", "an AccessLog entry must be provided")
		// We can't do any more validation without a Log
		return
	}
	if cfg.Log.LogFormat == aconfig.ACCESS_LOG_FORMAT_UNSPECIFIED {
		ce = ce.Appendf("Log.LogFormat", "a log format must be provided")
	}

	// TODO: validate custom templates when users can provide us with descriptors
	return
}

func (e *accessLogsWrapper) Close() error {
	return e.aspect.Close()
}

func (e *accessLogsWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator, ma APIMethodArgs) Output {
	labels, err := evalAll(e.labels, attrs, mapper)
	if err != nil {
		return Output{Status: status.WithError(fmt.Errorf("failed to eval labels for log %s with err: %s", e.name, err))}
	}

	templateVals, err := evalAll(e.templateExprs, attrs, mapper)
	if err != nil {
		return Output{Status: status.WithError(fmt.Errorf("failed to eval template expressions for log %s with err: %s", e.name, err))}
	}

	buf := pool.GetBuffer()
	if err := e.template.Execute(buf, templateVals); err != nil {
		pool.PutBuffer(buf)
		return Output{Status: status.WithError(fmt.Errorf("failed to execute payload template for log %s with err: %s", e.name, err))}
	}
	payload := buf.String()
	pool.PutBuffer(buf)

	entry := adapter.LogEntry{
		LogName:     e.name,
		Labels:      labels,
		TextPayload: payload,
	}
	if err := e.aspect.LogAccess([]adapter.LogEntry{entry}); err != nil {
		return Output{Status: status.WithError(fmt.Errorf("failed to log to %s with err: %s", e.name, err))}
	}
	return Output{Status: status.OK}
}
