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
	"time"

	rpc "github.com/googleapis/googleapis/google/rpc"

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

	accessLogsExecutor struct {
		name          string
		aspect        adapter.AccessLogsAspect
		labels        map[string]string // label name -> expression
		template      *template.Template
		templateExprs map[string]string // template variable -> expression
	}
)

const (
	// TODO: revisit when well-known attributes are defined.
	commonLogFormat = `{{or (.originIp) "-"}} - {{or (.sourceUser) "-"}} ` +
		`[{{or (.timestamp.Format "02/Jan/2006:15:04:05 -0700") "-"}}] "{{or (.method) "-"}} ` +
		`{{or (.url) "-"}} {{or (.protocol) "-"}}" {{or (.responseCode) "-"}} {{or (.responseSize) "-"}}`
	// TODO: revisit when well-known attributes are defined.
	combinedLogFormat = commonLogFormat + ` "{{or (.referer) "-"}}" "{{or (.user_agent) "-"}}"`
)

// newAccessLogsManager returns a manager for the access logs aspect.
func newAccessLogsManager() ReportManager {
	return accessLogsManager{}
}

func (m accessLogsManager) NewReportExecutor(c *cpb.Combined, a adapter.Builder, env adapter.Env, df descriptor.Finder) (ReportExecutor, error) {
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

	return &accessLogsExecutor{
		name:          cfg.LogName,
		aspect:        asp,
		labels:        cfg.Log.Labels,
		template:      tmpl,
		templateExprs: cfg.Log.TemplateExpressions,
	}, nil
}

func (accessLogsManager) Kind() config.Kind { return config.AccessLogsKind }
func (accessLogsManager) DefaultConfig() config.AspectParams {
	return &aconfig.AccessLogsParams{
		LogName: "access_log",
		Log: aconfig.AccessLogsParams_AccessLog{
			LogFormat: aconfig.COMMON,
		},
	}
}

func (accessLogsManager) ValidateConfig(c config.AspectParams, _ expr.Validator, _ descriptor.Finder) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.AccessLogsParams)
	if cfg.Log.LogFormat == aconfig.ACCESS_LOG_FORMAT_UNSPECIFIED {
		ce = ce.Appendf("Log.LogFormat", "a log format must be provided")
	}

	// TODO: validate custom templates when users can provide us with descriptors
	return
}

func (e *accessLogsExecutor) Close() error {
	return e.aspect.Close()
}

func (e *accessLogsExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator) rpc.Status {
	labels := permissiveEval(e.labels, attrs, mapper)
	templateVals := permissiveEval(e.templateExprs, attrs, mapper)

	buf := pool.GetBuffer()
	if err := e.template.Execute(buf, templateVals); err != nil {
		pool.PutBuffer(buf)
		return status.WithError(fmt.Errorf("failed to execute payload template for log %s with err: %s", e.name, err))
	}
	payload := buf.String()
	pool.PutBuffer(buf)

	entry := adapter.LogEntry{
		LogName:     e.name,
		Labels:      labels,
		TextPayload: payload,
	}
	if err := e.aspect.LogAccess([]adapter.LogEntry{entry}); err != nil {
		return status.WithError(fmt.Errorf("failed to log to %s with err: %s", e.name, err))
	}
	return status.OK
}

func permissiveEval(labels map[string]string, attrs attribute.Bag, mapper expr.Evaluator) map[string]interface{} {
	mappedVals := make(map[string]interface{}, len(labels))
	for name, exp := range labels {
		v, err := mapper.Eval(exp, attrs)
		if err == nil {
			mappedVals[name] = v
			continue
		}
		// TODO: timestamp is hardcoded here to match hardcoding in
		// templates and to get around current issues with existence of
		// attribute descriptors and expressiveness of config language
		if name == "timestamp" {
			mappedVals[name] = time.Now()
		}
	}
	return mappedVals
}
