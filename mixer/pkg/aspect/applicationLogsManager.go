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
	"text/template"
	"time"

	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"
	multierror "github.com/hashicorp/go-multierror"

	dpb "istio.io/api/mixer/v1/config/descriptor"
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
	// PayloadFormat describes the format of the LogEntry's payload.
	PayloadFormat int

	applicationLogsManager struct{}

	logInfo struct {
		format     PayloadFormat
		severity   string
		timestamp  string
		timeFormat string
		tmpl       *template.Template
		tmplExprs  map[string]string
		labels     map[string]string
	}

	applicationLogsExecutor struct {
		name     string
		aspect   adapter.ApplicationLogsAspect
		metadata map[string]*logInfo // descriptor_name -> info
	}
)

const (
	// Text describes a log entry whose TextPayload should be set.
	Text PayloadFormat = iota
	// JSON describes a log entry whose StructPayload should be set.
	JSON
)

// newApplicationLogsManager returns a manager for the application logs aspect.
func newApplicationLogsManager() ReportManager {
	return applicationLogsManager{}
}

func (applicationLogsManager) NewReportExecutor(c *cpb.Combined, a adapter.Builder, env adapter.Env, df descriptor.Finder) (ReportExecutor, error) {
	// TODO: look up actual descriptors by name and build an array
	cfg := c.Aspect.Params.(*aconfig.ApplicationLogsParams)
	metadata := make(map[string]*logInfo)
	for _, l := range cfg.Logs {
		// validation ensures both that the descriptor exists and that its template is parsable by the template library.
		d := df.GetLog(l.DescriptorName)
		t, _ := template.New(d.Name).Parse(d.LogTemplate)
		metadata[d.Name] = &logInfo{
			format:     payloadFormatFromProto(d.PayloadFormat),
			severity:   l.Severity,
			timestamp:  l.Timestamp,
			timeFormat: l.TimeFormat,
			tmpl:       t,
			tmplExprs:  l.TemplateExpressions,
			labels:     l.Labels,
		}
	}

	asp, err := a.(adapter.ApplicationLogsBuilder).NewApplicationLogsAspect(env, c.Builder.Params.(adapter.Config))
	if err != nil {
		return nil, fmt.Errorf("failed to construct application logs aspect with err: %v", err)
	}

	return &applicationLogsExecutor{
		name:     cfg.LogName,
		aspect:   asp,
		metadata: metadata,
	}, nil
}

func (applicationLogsManager) Kind() config.Kind { return config.ApplicationLogsKind }
func (applicationLogsManager) DefaultConfig() config.AspectParams {
	return &aconfig.ApplicationLogsParams{LogName: "istio_log"}
}

// TODO: validation of timestamp format
func (applicationLogsManager) ValidateConfig(c config.AspectParams, v expr.Validator, df descriptor.Finder) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.ApplicationLogsParams)
	if cfg.LogName == "" {
		ce = ce.Appendf("LogName", "no log name provided")
	}

	for _, log := range cfg.Logs {
		desc := df.GetLog(log.DescriptorName)
		if desc == nil {
			ce = ce.Appendf("Logs", "could not find a descriptor for the log '%s'", log.DescriptorName)
			continue // we can't do any other validation without the descriptor
		}

		if err := v.AssertType(log.Severity, df, dpb.STRING); err != nil {
			ce = ce.Appendf(fmt.Sprintf("Logs[%s].Severity", log.DescriptorName), "failed type checking with err: %v", err)
		}
		if err := v.AssertType(log.Timestamp, df, dpb.TIMESTAMP); err != nil {
			ce = ce.Appendf(fmt.Sprintf("Logs[%s].Timestamp", log.DescriptorName), "failed type checking with err: %v", err)
		}
		ce = ce.Extend(validateLabels(fmt.Sprintf("Logs[%s].Labels", log.DescriptorName), log.Labels, desc.Labels, v, df))
		ce = ce.Extend(validateTemplateExpressions(fmt.Sprintf("LogDescriptor[%s].TemplateExpressions", desc.Name), log.TemplateExpressions, v, df))

		// TODO: how do we validate the log.TemplateExpressions against desc.LogTemplate? We can't just `Execute` the template
		// against the expressions: while the keys to the template may be correct, the values will be wrong which could result
		// in non-nil error returns even when things would be valid at runtime.
		if _, err := template.New(desc.Name).Parse(desc.LogTemplate); err != nil {
			ce = ce.Appendf(fmt.Sprintf("LogDescriptor[%s].LogTemplate", desc.Name), "failed to parse template with err: %v", err)
		}
	}
	return
}

func (e *applicationLogsExecutor) Close() error { return e.aspect.Close() }

func (e *applicationLogsExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator) rpc.Status {
	result := &multierror.Error{}
	var entries []adapter.LogEntry

	for name, md := range e.metadata {
		labels, err := evalAll(md.labels, attrs, mapper)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to eval labels for log entry '%s' with err: %s", name, err))
			continue
		}

		templateVals, err := evalAll(md.tmplExprs, attrs, mapper)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to eval template values for log entry '%s' with err: %s", name, err))
			continue
		}

		buf := pool.GetBuffer()
		err = md.tmpl.Execute(buf, templateVals)
		if err != nil {
			pool.PutBuffer(buf)
			result = multierror.Append(result, fmt.Errorf(
				"failed to construct payload string for log entry '%s' with template execution err: %s", name, err))
			continue
		}

		sevStr, err := mapper.EvalString(md.severity, attrs)
		if err != nil {
			result = multierror.Append(result,
				fmt.Errorf("failed to eval severity for log entry '%s', continuing with DEFAULT severity. Eval err: %s", name, err))
			sevStr = ""
		}
		// If we can't parse the string we'll get the default severity, so we don't care about the success return.
		severity, _ := adapter.SeverityByName(sevStr)

		et, err := mapper.Eval(md.timestamp, attrs)
		if err != nil {
			result = multierror.Append(result,
				fmt.Errorf("failed to eval time for log entry '%s', continuing with time.Now(). Eval err: %s", name, err))
			et = time.Now()
		}
		t, _ := et.(time.Time) // we don't check the cast because expression type checking ensures we get a time.

		entry := adapter.LogEntry{
			LogName:   e.name,
			Labels:    labels,
			Timestamp: t.Format(md.timeFormat),
			Severity:  severity,
		}

		switch md.format {
		case Text:
			entry.TextPayload = buf.String()
		case JSON:
			if err := json.Unmarshal(buf.Bytes(), &entry.StructPayload); err != nil {
				result = multierror.Append(result, fmt.Errorf("failed to unmarshall json payload for log entry %s with err: %s", name, err))
				continue
			}
		}
		pool.PutBuffer(buf)

		entries = append(entries, entry)
	}
	if len(entries) > 0 {
		if err := e.aspect.Log(entries); err != nil {
			return status.WithError(err)
		}
	}

	err := result.ErrorOrNil()
	if glog.V(4) {
		glog.Infof("completed execution of application logging adapter '%s' for %d entries with errs: %v", e.name, len(entries), err)
	}
	if err != nil {
		return status.WithError(err)
	}
	return status.OK
}

func payloadFormatFromProto(format dpb.LogEntryDescriptor_PayloadFormat) PayloadFormat {
	switch format {
	case dpb.JSON:
		return JSON
	case dpb.TEXT:
		fallthrough
	default:
		return Text
	}
}
