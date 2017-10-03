// Copyright 2017 Istio Authors.
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

package descriptor

import (
	"bytes"
	"flag"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
)

type (
	getter func(Finder) proto.Message

	cases []struct {
		name string
		cfg  *pb.GlobalConfig
		get  getter
		out  interface{}
	}
)

var (
	logDesc = dpb.LogEntryDescriptor{
		Name:          "log",
		PayloadFormat: dpb.TEXT,
		LogTemplate:   "{{}}",
		Labels:        make(map[string]dpb.ValueType),
	}

	getLog = func(k string) getter {
		return func(f Finder) proto.Message {
			return f.GetLog(k)
		}
	}

	metricDesc = dpb.MetricDescriptor{
		Name:   "metric",
		Labels: make(map[string]dpb.ValueType),
	}

	getMetric = func(k string) getter {
		return func(f Finder) proto.Message {
			return f.GetMetric(k)
		}
	}

	monitoredResourceDesc = dpb.MonitoredResourceDescriptor{
		Name:   "mr",
		Labels: make(map[string]dpb.ValueType),
	}

	getMR = func(k string) getter {
		return func(f Finder) proto.Message {
			return f.GetMonitoredResource(k)
		}
	}

	principalDesc = dpb.PrincipalDescriptor{
		Name:   "principal",
		Labels: make(map[string]dpb.ValueType),
	}

	getPrincipal = func(k string) getter {
		return func(f Finder) proto.Message {
			return f.GetPrincipal(k)
		}
	}

	quotaDesc = dpb.QuotaDescriptor{
		Name:   "quota",
		Labels: make(map[string]dpb.ValueType),
	}

	getQuota = func(k string) getter {
		return func(f Finder) proto.Message {
			return f.GetQuota(k)
		}
	}

	attributeDesc = map[string]*pb.AttributeManifest_AttributeInfo{
		"attr": {ValueType: dpb.BOOL},
	}

	getAttr = func(k string) getter {
		return func(f Finder) proto.Message {
			return f.GetAttribute(k)
		}
	}
)

func TestGetLog(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{Logs: []*dpb.LogEntryDescriptor{&logDesc}}, getLog("log"), &logDesc},
		{"missing", &pb.GlobalConfig{Logs: []*dpb.LogEntryDescriptor{&logDesc}}, getLog("foo"), nil},
		{"no logs", &pb.GlobalConfig{}, getLog("log"), nil},
	})
}

func TestGetMetric(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{Metrics: []*dpb.MetricDescriptor{&metricDesc}}, getMetric("metric"), &metricDesc},
		{"missing", &pb.GlobalConfig{Metrics: []*dpb.MetricDescriptor{&metricDesc}}, getMetric("foo"), nil},
		{"no metrics", &pb.GlobalConfig{}, getMetric("metric"), nil},
	})
}

func TestGetMonitoredResource(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{MonitoredResources: []*dpb.MonitoredResourceDescriptor{&monitoredResourceDesc}}, getMR("mr"), &monitoredResourceDesc},
		{"missing", &pb.GlobalConfig{MonitoredResources: []*dpb.MonitoredResourceDescriptor{&monitoredResourceDesc}}, getMR("foo"), nil},
		{"no MRs", &pb.GlobalConfig{}, getMR("mr"), nil},
	})
}

func TestGetPrincipal(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{Principals: []*dpb.PrincipalDescriptor{&principalDesc}}, getPrincipal("principal"), &principalDesc},
		{"missing", &pb.GlobalConfig{Principals: []*dpb.PrincipalDescriptor{&principalDesc}}, getPrincipal("foo"), nil},
		{"no principals", &pb.GlobalConfig{}, getPrincipal("principal"), nil},
	})
}

func TestGetQuota(t *testing.T) {
	execute(t, cases{
		{"empty", &pb.GlobalConfig{Quotas: []*dpb.QuotaDescriptor{&quotaDesc}}, getQuota("quota"), &quotaDesc},
		{"missing", &pb.GlobalConfig{Quotas: []*dpb.QuotaDescriptor{&quotaDesc}}, getQuota("foo"), nil},
		{"no quotas", &pb.GlobalConfig{}, getQuota("quota"), nil},
	})
}

func TestGetAttribute(t *testing.T) {
	mkcfg := func(descs map[string]*pb.AttributeManifest_AttributeInfo) *pb.GlobalConfig {
		return &pb.GlobalConfig{Manifests: []*pb.AttributeManifest{{Attributes: descs}}}
	}

	execute(t, cases{
		{"empty", mkcfg(attributeDesc), getAttr("attr"), attributeDesc["attr"]},
		{"missing", mkcfg(attributeDesc), getAttr("foo"), nil},
		{"no attributes", &pb.GlobalConfig{}, getAttr("attr"), nil},
	})
}

func execute(t *testing.T, tests cases) {
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			f := NewFinder(tt.cfg)
			d := tt.get(f)
			if d == nil && tt.out != nil {
				t.Fatalf("tt.fn() = _, false; expected descriptor %v", tt.out)
			}
			if tt.out != nil && !reflect.DeepEqual(d, tt.out) {
				t.Fatalf("tt.fn() = %v; expected descriptor %v", d, tt.out)
			}
		})
	}
}

func testParser(mutations map[string]interface{}, wantErr string, t *testing.T) {
	m := map[string]interface{}{}
	var ba []byte
	var err error
	if err = yaml.Unmarshal([]byte(allGoodConfig), &m); err != nil {
		t.Fatalf("unable unmarshal %v with: %v", allGoodConfig, err)
	}

	for path, val := range mutations {
		mutate(m, path, val)
	}

	if ba, err = yaml.Marshal(m); err != nil {
		t.Fatalf("unable to marshal %v with: %v", m, err)
	}

	_, ce := Parse(string(ba))
	gotErr := ""
	if ce != nil {
		gotErr = ce.Error()
	}

	if !strings.Contains(gotErr, wantErr) {
		t.Errorf("got %s\nwant %s", gotErr, wantErr)
	}

}

func checkError(got error, want string, t *testing.T) {
	msg := "nothing"
	if got != nil {
		msg = got.Error()
	}
	if !strings.Contains(msg, want) {
		t.Errorf("got %s\nwant %s", msg, want)
	}
}

func TestParse_BadInput(t *testing.T) {
	t.Run("Bad_Yaml", func(t *testing.T) {
		_, err := Parse("<badyaml></badyaml>")
		checkError(err, "descriptorConfig: error unmarshaling JSON", t)
	})

	t.Run("NonJsonInput", func(t *testing.T) {
		nonjson := make(chan int)
		err := updateMsg("bad", nonjson, nil, nil, false)
		checkError(err, "unsupported type", t)
	})
}

func TestParseErrors(t *testing.T) {
	for _, tt := range []struct {
		m       map[string]interface{}
		wantErr string
	}{
		{map[string]interface{}{
			"manifests[0].attributes[source].value_type": "WRONG_STRING"},
			"manifests[0].attributes[source]: unknown value"},
		{map[string]interface{}{
			"quotas[0].unknown_attribute": "unknown_value"},
			"quotas[0]: unknown field"},
		{map[string]interface{}{
			"manifests[0].unknown_attribute": "unknown_value"},
			"manifests[0]: unknown field"},
	} {
		t.Run(tt.wantErr, func(tx *testing.T) {
			testParser(tt.m, tt.wantErr, tx)
		})
	}
}

// ensure that Parse and jsonpb.Parse are equivalent
func TestParseValid(t *testing.T) {
	dcfg, ce := Parse(allGoodConfig)
	if ce != nil {
		t.Fatalf("Unexpected error %s", ce)
	}

	jsonConfig, err := yaml.YAMLToJSON([]byte(allGoodConfig))
	if err != nil {
		t.Fatalf("could not convert to json %s", err)
	}

	cfg := &pb.GlobalConfig{}
	if err := jsonpb.Unmarshal(bytes.NewReader(jsonConfig), cfg); err != nil {
		t.Fatalf("unable to parse %s", err)
	}
	m := jsonpb.Marshaler{
		Indent: " ",
	}
	sCsg, _ := m.MarshalToString(cfg)
	sDcfg, _ := m.MarshalToString(dcfg)
	if sCsg != sDcfg {
		t.Fatalf("%s != %s", sCsg, sDcfg)
	}
}

var sepRegex = regexp.MustCompile(`\[|\]|\.`)

// mutates the json at given path with val
// nolint: unparam
func mutate(m interface{}, path string, val interface{}) interface{} {
	var idx int
	var key string
	var err error
	var qa []interface{}
	var qm map[string]interface{}

	v := m

	tokens := sepRegex.Split(path, -1)
	for tidx, tok := range tokens {
		if len(tok) == 0 {
			continue
		}
		idx, err = strconv.Atoi(tok)
		qa, qm = nil, nil
		if err == nil { // array
			qa, _ = v.([]interface{})
			if qa == nil {
				panic(fmt.Sprintf("%s is not an array; all tokens: %s", tokens[:tidx], tokens))
			}
			v = qa[idx]
		} else { // map
			qm, _ = v.(map[string]interface{})
			if qm == nil {
				panic(fmt.Sprintf("%s is not a map", tokens[:tidx]))
			}
			v = qm[tok]
			key = tok
		}
	}

	if qm != nil {
		qm[key] = val
	} else {
		qa[idx] = val
	}
	return m
}

const allGoodConfig = `
revision: "2022"
manifests:
  - name: istio-proxy
    revision: "1"
    attributes:
      source:
        value_type: STRING

# Enums as struct fields can be symbolic names.
# However enums inside maps *cannot* be symbolic names.
metrics:
  - name: request_count
    kind: COUNTER
    value: INT64
    description: request count by source, target, service, and code
    labels:
      source: 1 # STRING
      target: 1 # STRING
      service: 1 # STRING
      method: 1 # STRING
      response_code: 2 # INT64
  - name: request_latency
    kind: COUNTER
    value: DURATION
    description: request latency by source, target, and service
    labels:
      source: 1 # STRING
      target: 1 # STRING
      service: 1 # STRING
      method: 1 # STRING
      response_code: 2 # INT64

quotas:
- name: RequestCount
  rate_limit: true

logs:
  - name: accesslog.common
    display_name: Apache Common Log Format
    log_template: '{{or (.originIp) "-"}} - {{or (.sourceUser) "-"}} '
    labels:
      originIp: 6 # IP_ADDRESS
      sourceUser: 1 # STRING
      timestamp: 5 # TIMESTAMP
      method: 1 # STRING
      url: 1 # STRING
      protocol: 1 # STRING
      responseCode: 2 # INT64
      responseSize: 2 # INT64
  - name: accesslog.combined
    display_name: Apache Combined Log Format
    log_template: '{{or (.originIp) "-"}} - {{or (.sourceUser) "-"}} '
    labels:
      originIp: 6 # IP_ADDRESS
      sourceUser: 1 # STRING
      timestamp: 5 # TIMESTAMP
      method: 1 # STRING
      url: 1 # STRING
      protocol: 1 # STRING
      responseCode: 2 # INT64
      responseSize: 2 # INT64
      referer: 1 # STRING
      userAgent: 1 # STRING
`

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
