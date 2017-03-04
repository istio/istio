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
	"text/template"
	"time"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

type (
	accessLogsManager struct{}

	accessLogsWrapper struct {
		logName   string
		inputs    map[string]string // map from param to expr
		aspect    adapter.AccessLogsAspect
		attrNames []string
		template  *template.Template
	}
)

const (
	// TODO: revisit when well-known attributes are defined.
	commonLogFormat = `{{or (.originIp) "-"}} - {{or (.source_user) "-"}} ` +
		`[{{or (.timestamp.Format "02/Jan/2006:15:04:05 -0700") "-"}}] "{{or (.apiMethod) "-"}} ` +
		`{{or (.url) "-"}} {{or (.protocol) "-"}}" {{or (.responseCode) "-"}} {{or (.responseSize) "-"}}`
	// TODO: revisit when well-known attributes are defined.
	combinedLogFormat = commonLogFormat + ` "{{or (.referer) "-"}}" "{{or (.user_agent) "-"}}"`
)

var (
	// TODO: revisit when well-known attributes are defined
	commonLogAttributes = []string{"originIp", "source_user", "timestamp", "apiMethod", "url", "protocol", "responseCode", "responseSize"}
	// TODO: revisit when well-known attributes are defined
	combinedLogAttributes = append(commonLogAttributes, "referer", "user_agent")
)

// newAccessLogsManager returns a manager for the access logs aspect.
func newAccessLogsManager() Manager {
	return accessLogsManager{}
}

func (m accessLogsManager) NewAspect(c *config.Combined, a adapter.Builder, env adapter.Env) (Wrapper, error) {
	var aspect adapter.AccessLogsAspect
	var err error
	var tmpl *template.Template

	logCfg := c.Aspect.Params.(*aconfig.AccessLogsParams)
	logName := logCfg.LogName
	logFormat := logCfg.LogFormat
	var attrNames []string
	var templateStr string
	switch logFormat {
	case aconfig.COMMON:
		attrNames = commonLogAttributes
		templateStr = commonLogFormat
	case aconfig.COMBINED:
		attrNames = combinedLogAttributes
		templateStr = combinedLogFormat
	case aconfig.CUSTOM:
		fallthrough
	default:
		templateStr = logCfg.CustomLogTemplate
		attrNames = logCfg.Attributes
	}

	// should never result in error, as this should fail ValidateConfig()
	if tmpl, err = template.New("accessLogsTemplate").Parse(templateStr); err != nil {
		return nil, err
	}

	if aspect, err = a.(adapter.AccessLogsBuilder).NewAccessLogsAspect(env, c.Builder.Params.(adapter.AspectConfig)); err != nil {
		return nil, err
	}

	return &accessLogsWrapper{
		logName,
		c.Aspect.GetInputs(),
		aspect,
		attrNames,
		tmpl,
	}, nil
}

func (accessLogsManager) Kind() Kind { return AccessLogsKind }
func (accessLogsManager) DefaultConfig() adapter.AspectConfig {
	return &aconfig.AccessLogsParams{
		LogName:   "access_log",
		LogFormat: aconfig.COMMON,
		// WARNING: we cannot set default attributes here, based on
		// the params -> proto merge logic. These will override
		// all other values. This should be mitigated by the move
		// away from structpb-based config merging.
	}
}

func (accessLogsManager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.AccessLogsParams)
	if cfg.LogFormat != aconfig.CUSTOM {
		return nil
	}
	tmplStr := cfg.CustomLogTemplate
	if _, err := template.New("test").Parse(tmplStr); err != nil {
		return ce.Appendf("CustomLogTemplate", "could not parse template string: %v", err)
	}
	return nil
}

func (e *accessLogsWrapper) Close() error {
	return e.aspect.Close()
}

func (e *accessLogsWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator, ma APIMethodArgs) Output {
	// TODO: would be nice if we could use a mutable.Bag here and could pass it around
	// labels holds the generated attributes from mapper
	labels := make(map[string]interface{})
	for attr, e := range e.inputs {
		if val, err := mapper.Eval(e, attrs); err == nil {
			labels[attr] = val
		}
		// TODO: throw error on failed mapping?
	}

	// TODO: better way to ensure timestamp is available if not supplied
	// in Report() requests.
	if _, found := labels["timestamp"]; !found {
		labels["timestamp"] = time.Now()
	}

	entry := adapter.LogEntry{
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

	if len(entry.Labels) == 0 {
		// don't write empty access logs
		return Output{Status: status.OK}
	}

	buf := pool.GetBuffer()
	if err := e.template.Execute(buf, entry.Labels); err != nil {
		return Output{Status: status.WithError(err)}
	}
	entry.TextPayload = buf.String()
	pool.PutBuffer(buf)
	if err := e.aspect.LogAccess([]adapter.LogEntry{entry}); err != nil {
		return Output{Status: status.WithError(err)}
	}
	return Output{Status: status.OK}
}
