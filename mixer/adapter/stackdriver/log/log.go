// Copyright Istio Authors
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
	"net"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	xctx "golang.org/x/net/context"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/api/monitoredres"

	istio_policy_v1beta1 "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/pkg/pool"
)

type (
	makeClientFn     func(ctx xctx.Context, projectID string, opts ...option.ClientOption) (*logging.Client, error)
	makeSyncClientFn func(ctx xctx.Context, projectID string, opts ...option.ClientOption) (*logadmin.Client, error)

	logFn   func(logging.Entry)
	flushFn func() error

	builder struct {
		makeClient     makeClientFn
		makeSyncClient makeSyncClientFn
		types          map[string]*logentry.Type
		mg             helper.MetadataGenerator
		cfg            *config.Params
	}

	info struct {
		labels []string
		tmpl   *template.Template
		req    *config.Params_LogInfo_HttpRequestMapping
		log    logFn
		flush  flushFn
	}

	handler struct {
		now func() time.Time // used for testing

		l                  adapter.Logger
		client, syncClient io.Closer
		info               map[string]info
		md                 helper.Metadata
		types              map[string]*logentry.Type
	}
)

var (
	// compile-time assertion that we implement the interfaces we promise
	_ logentry.HandlerBuilder = &builder{}
	_ logentry.Handler        = &handler{}
)

// NewBuilder returns a builder implementing the logentry.HandlerBuilder interface.
func NewBuilder(mg helper.MetadataGenerator) logentry.HandlerBuilder {
	return &builder{makeClient: logging.NewClient, makeSyncClient: logadmin.NewClient, mg: mg}
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
	md := b.mg.GenerateMetadata()
	if cfg.ProjectId == "" {
		// Try to fill project id with Metadata if it is not provided.
		cfg.ProjectId = md.ProjectID
	}
	client, err := b.makeClient(context.Background(), cfg.ProjectId, helper.ToOpts(cfg, env.Logger())...)
	if err != nil {
		return nil, fmt.Errorf("failed to create stackdriver logging client: %v", err)
	}
	client.OnError = func(err error) {
		_ = logger.Errorf("Stackdriver logger failed with: %v", err)
	}

	syncClient, err := b.makeSyncClient(context.Background(), cfg.ProjectId, helper.ToOpts(cfg, env.Logger())...)
	if err != nil {
		_ = logger.Errorf("failed to create stackdriver sink logging client: %v", err)
		syncClient = nil
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

		if log.SinkInfo != nil {
			sink := &logadmin.Sink{
				ID:          log.SinkInfo.Id,
				Destination: log.SinkInfo.Destination,
				Filter:      log.SinkInfo.Filter,
			}
			ctx := context.Background()
			var sinkErr error
			// first try to CreateSink, if error is AlreadyExists then update that sink.
			if _, sinkErr = syncClient.CreateSinkOpt(ctx, sink,
				logadmin.SinkOptions{UniqueWriterIdentity: log.SinkInfo.UniqueWriterIdentity}); isAlreadyExists(sinkErr) {
				_, sinkErr = syncClient.UpdateSinkOpt(ctx, sink,
					logadmin.SinkOptions{UpdateDestination: log.SinkInfo.UpdateDestination,
						UpdateFilter:          log.SinkInfo.UpdateFilter,
						UpdateIncludeChildren: log.SinkInfo.UpdateIncludeChildren})
			}
			if sinkErr != nil {
				logger.Warningf("failed to create/update stackdriver logging sink: %v", sinkErr)
			}
		}

		infos[name] = info{
			labels: log.LabelNames,
			tmpl:   tmpl,
			req:    log.HttpMapping,
			log:    client.Logger(name).Log,
			flush:  client.Logger(name).Flush,
		}
	}
	return &handler{client: client, syncClient: syncClient, now: time.Now, l: logger, info: infos, md: md, types: b.types}, nil
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

		var logEntryTypes map[string]istio_policy_v1beta1.ValueType
		if typeInfo, found := h.types[v.Name]; found {
			logEntryTypes = typeInfo.Variables
		}
		e := logging.Entry{
			Timestamp:   h.now(), // TODO: use timestamp on Instance when timestamps work
			Severity:    logging.ParseSeverity(v.Severity),
			Labels:      toLabelMap(linfo.labels, v.Variables, logEntryTypes),
			Payload:     payload,
			HTTPRequest: toReq(linfo.req, v.Variables),
		}

		// If we don't set a resource the SDK will populate a global resource for us.
		if v.MonitoredResourceType != "" {
			labels := helper.ToStringMap(v.MonitoredResourceDimensions)
			h.md.FillProjectMetadata(labels)
			e.Resource = &monitoredres.MonitoredResource{
				Type:   v.MonitoredResourceType,
				Labels: labels,
			}
		}
		linfo.log(e)
	}

	for name, linfo := range h.info {
		err := linfo.flush()
		if err != nil {
			h.l.Warningf("failed to flush log %s: %v", name, err)
		}
	}
	return nil
}

func (h *handler) Close() error {
	if h.syncClient != nil {
		if err := h.syncClient.Close(); err != nil {
			h.l.Warningf("Failed to close sync client %v", err)
		}
	}

	return h.client.Close()
}

func toLabelMap(names []string, variables map[string]interface{}, logEntryTypes map[string]istio_policy_v1beta1.ValueType) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		v := variables[name]
		switch vt := v.(type) {
		case string:
			out[name] = stripBadUTF8(vt)
		case []byte:
			if logEntryTypes[name] == istio_policy_v1beta1.IP_ADDRESS {
				out[name] = net.IP(vt).String()
			} else {
				out[name] = stripBadUTF8(fmt.Sprintf("%v", vt))
			}
		default:
			out[name] = stripBadUTF8(fmt.Sprintf("%v", vt))
		}
	}
	return out
}

func stripBadUTF8(input string) string {
	if utf8.ValidString(input) {
		return input
	}

	out := make([]rune, 0, len(input))
	for _, r := range input {
		if r == utf8.RuneError {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func toReq(mapping *config.Params_LogInfo_HttpRequestMapping, variables map[string]interface{}) *logging.HTTPRequest {
	if mapping == nil {
		return nil
	}

	reqURL := &url.URL{}
	if variables[mapping.Url] != nil {
		if u, err := url.Parse(variables[mapping.Url].(string)); err == nil {
			reqURL = u
		}
	}
	method := ""
	if variables[mapping.Method] != nil {
		method = variables[mapping.Method].(string)
	}
	httpHeaders := make(http.Header)
	if variables[mapping.UserAgent] != nil {
		httpHeaders.Add("User-Agent", variables[mapping.UserAgent].(string))
	}
	if variables[mapping.Referer] != nil {
		httpHeaders.Add("Referer", variables[mapping.Referer].(string))
	}
	// Required to make the Stackdriver client lib not barf.
	req := &logging.HTTPRequest{
		Request: &http.Request{URL: reqURL, Method: method, Header: httpHeaders},
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
	if status, ok := code.(int64); ok {
		req.Status = int(status)
	}

	l := variables[mapping.Latency]
	if latency, ok := l.(time.Duration); ok {
		req.Latency = latency
	}

	req.LocalIP = adapter.Stringify(variables[mapping.LocalIp])
	req.RemoteIP = adapter.Stringify(variables[mapping.RemoteIp])
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

func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "AlreadyExists")
}
