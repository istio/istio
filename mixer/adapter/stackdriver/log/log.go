// Copyright 2017 the Istio Authors.
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

package log

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"text/template"
	"time"

	"cloud.google.com/go/logging"
	xctx "golang.org/x/net/context"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/api/monitoredres"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/template/logentry"
)

type (
	makeClientFn func(ctx xctx.Context, projectID string, opts ...option.ClientOption) (*logging.Client, error)

	logFn func(logging.Entry)

	builder struct {
		makeClient makeClientFn
		types      map[string]*logentry.Type
		cfg        *config.Params
	}

	info struct {
		labels []string
		tmpl   *template.Template
		req    *config.Params_LogInfo_HttpRequestMapping
		log    logFn
	}

	handler struct {
		now func() time.Time // used for testing

		l      adapter.Logger
		client io.Closer
		info   map[string]info
	}
)

var (
	// compile-time assertion that we implement the interfaces we promise
	_ logentry.HandlerBuilder = &builder{}
	_ logentry.Handler        = &handler{}
)

// NewBuilder returns a builder implementing the logentry.HandlerBuilder interface.
func NewBuilder() logentry.HandlerBuilder {
	return &builder{makeClient: logging.NewClient}
}

func (b *builder) SetLogEntryTypes(types map[string]*logentry.Type) {
	b.types = types
}
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.cfg = cfg.(*config.Params)
}
func (b *builder) Validate() *adapter.ConfigErrors {
	return nil
}

func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	logger := env.Logger()
	cfg := b.cfg
	client, err := b.makeClient(context.Background(), cfg.ProjectId, helper.ToOpts(cfg)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create stackdriver logging client: %v", err)
	}
	client.OnError = func(err error) {
		_ = logger.Errorf("Stackdriver logger failed with: %v", err)
	}

	infos := make(map[string]info)
	for name, log := range cfg.LogInfo {
		_, found := b.types[name]
		if !found {
			logger.Infof("configured with log info about %s which is not an Istio log", name)
			continue
		}
		tmpl, err := template.New(name).Parse(log.PayloadTemplate)
		if err != nil {
			_ = logger.Errorf("failed to evaluate template for log %s, skipping: %v", name, err)
			continue
		}
		infos[name] = info{
			labels: log.LabelNames,
			tmpl:   tmpl,
			req:    log.HttpMapping,
			log:    client.Logger(name).Log,
		}
	}
	return &handler{client: client, now: time.Now, l: logger, info: infos}, nil
}

func (h *handler) HandleLogEntry(_ context.Context, values []*logentry.Instance) error {
	for _, v := range values {
		linfo, found := h.info[v.Name]
		if !found {
			h.l.Warningf("got instance for unknown log '%s', skipping", v.Name)
			continue
		}

		buf := pool.GetBuffer()
		if err := linfo.tmpl.Execute(buf, v.Variables); err != nil {
			// We'll just continue on with an empty payload for this entry - we could still be populating the HTTP req with valuable info, for example.
			_ = h.l.Errorf("failed to execute template for log '%s': %v", v.Name, err)
		}
		payload := buf.String()
		pool.PutBuffer(buf)

		e := logging.Entry{
			Timestamp:   h.now(), // TODO: use timestamp on Instance when timestamps work
			Severity:    logging.ParseSeverity(v.Severity),
			Labels:      toLabelMap(linfo.labels, v.Variables),
			Payload:     payload,
			HTTPRequest: toReq(linfo.req, v.Variables),
		}

		// If we don't set a resource the SDK will populate a global resource for us.
		if v.MonitoredResourceType != "" {
			e.Resource = &monitoredres.MonitoredResource{
				Type:   v.MonitoredResourceType,
				Labels: helper.ToStringMap(v.MonitoredResourceDimensions),
			}
		}
		linfo.log(e)
	}
	return nil
}

func (h *handler) Close() error {
	return h.client.Close()
}

func toLabelMap(names []string, variables map[string]interface{}) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		v := variables[name]
		switch vt := v.(type) {
		case string:
			out[name] = vt
		default:
			out[name] = fmt.Sprintf("%v", v)
		}
	}
	return out
}

func toReq(mapping *config.Params_LogInfo_HttpRequestMapping, variables map[string]interface{}) *logging.HTTPRequest {
	if mapping == nil {
		return nil
	}

	// Required to make the Stackdriver client lib not barf.
	// TODO: see if we can plumb the URL through to here to populate this meaningfully.
	req := &logging.HTTPRequest{
		Request: &http.Request{URL: &url.URL{}},
	}

	reqs := variables[mapping.RequestSize]
	if reqsize, ok := toInt64(reqs); ok {
		req.RequestSize = reqsize
	}

	resps := variables[mapping.ResponseSize]
	if respsize, ok := toInt64(resps); ok {
		req.ResponseSize = respsize
	}

	code := variables[mapping.Status]
	if status, ok := code.(int); ok {
		req.Status = status
	}

	l := variables[mapping.Latency]
	if latency, ok := l.(time.Duration); ok {
		req.Latency = latency
	}

	lip := variables[mapping.LocalIp]
	if localip, ok := lip.(string); ok {
		req.LocalIP = localip
	}

	rip := variables[mapping.RemoteIp]
	if remoteip, ok := rip.(string); ok {
		req.RemoteIP = remoteip
	}
	return req
}

func toInt64(v interface{}) (int64, bool) {
	// case int, int8, int16, ...: return int64(i), true
	// does not compile because go can't handle the type conversion for multiple types in a single branch.
	switch i := v.(type) {
	case int:
		return int64(i), true
	case int64:
		return i, true
	default:
		return 0, false
	}
}
