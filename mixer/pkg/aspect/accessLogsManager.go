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

// TODO: there are several hard-coded but end-user-configurable things in this file we need to work to eliminiate:
// - default config assumes a log descriptor named "common" is available
// - if we get a label or template param named exactly "timestamp" we populate the value with time.Now()

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

// newAccessLogsManager returns a manager for the access logs aspect.
func newAccessLogsManager() ReportManager {
	return accessLogsManager{}
}

func (m accessLogsManager) NewReportExecutor(c *cpb.Combined, createAspect CreateAspectFunc, env adapter.Env,
	df descriptor.Finder, _ string) (ReportExecutor, error) {
	cfg := c.Aspect.Params.(*aconfig.AccessLogsParams)

	// validation ensures both that the descriptor exists and that its template is parsable by the template library.
	desc := df.GetLog(cfg.Log.DescriptorName)
	tmpl, _ := template.New("accessLogsTemplate").Parse(desc.LogTemplate)

	out, err := createAspect(env, c.Builder.Params.(adapter.Config))
	if err != nil {
		return nil, fmt.Errorf("failed to create aspect for log %s: %v", cfg.LogName, err)
	}
	asp, ok := out.(adapter.AccessLogsAspect)
	if !ok {
		return nil, fmt.Errorf("wrong aspect type returned after creation; expected AccessLogsAspect: %#v", out)
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
			DescriptorName: "common",
		},
	}
}

func (accessLogsManager) ValidateConfig(c config.AspectParams, tc expr.TypeChecker, df descriptor.Finder) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.AccessLogsParams)
	if cfg.LogName == "" {
		ce = ce.Appendf("logName", "no log name provided")
	}

	desc := df.GetLog(cfg.Log.DescriptorName)
	if desc == nil {
		// nor can we do more validation without a descriptor
		return ce.Appendf("accessLogs", "could not find a descriptor for the access log '%s'", cfg.Log.DescriptorName)
	}
	ce = ce.Extend(validateLabels(fmt.Sprintf("accessLogs[%s].labels", cfg.Log.DescriptorName), cfg.Log.Labels, desc.Labels, tc, df))
	ce = ce.Extend(validateTemplateExpressions(fmt.Sprintf("logDescriptor[%s].templateExpressions", desc.Name), cfg.Log.TemplateExpressions, tc, df))

	// TODO: how do we validate the log.TemplateExpressions against desc.LogTemplate? We can't just `Execute` the template
	// against the expressions: while the keys to the template may be correct, the values will be wrong which could result
	// in non-nil error returns even when things would be valid at runtime.
	if _, err := template.New(desc.Name).Parse(desc.LogTemplate); err != nil {
		ce = ce.Appendf(fmt.Sprintf("logDescriptor[%s].logTemplate", desc.Name), "failed to parse template: %v", err)
	}
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
		return status.WithError(fmt.Errorf("failed to execute payload template for log %s: %v", e.name, err))
	}
	payload := buf.String()
	pool.PutBuffer(buf)

	entry := adapter.LogEntry{
		LogName:     e.name,
		Labels:      labels,
		TextPayload: payload,
	}
	if err := e.aspect.LogAccess([]adapter.LogEntry{entry}); err != nil {
		return status.WithError(fmt.Errorf("failed to log to %s: %v", e.name, err))
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
