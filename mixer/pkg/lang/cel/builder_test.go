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

package cel

import (
	"errors"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker"
	"github.com/google/cel-go/common/debug"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"istio.io/api/policy/v1beta1"
	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/pkg/attribute"
)

func compatTest(test ilt.TestInfo) func(t *testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		finder := attribute.NewFinder(test.Conf())
		builder := NewBuilder(finder, LegacySyntaxCEL)
		ex, typ, err := builder.Compile(test.E)

		if err != nil {
			if test.CompileErr != "" {
				return
			}
			t.Fatalf("unexpected compile error %v for %s", err, ex)
		}

		// timestamp(2) is not a compile error in CEL
		// division is also supported by CEL
		if test.CompileErr != "" {
			return
		}

		if test.Type != typ {
			t.Errorf("expected type %s, got %s", test.Type, typ)
		}

		b := ilt.NewFakeBag(test.I)

		out, err := ex.Evaluate(b)
		if err != nil {
			if test.Err != "" {
				return
			}
			if test.CEL != nil {
				if expectedErr, ok := test.CEL.(error); ok && strings.Contains(err.Error(), expectedErr.Error()) {
					return
				}
			}
			t.Fatalf("unexpected evaluation error: %v", err)
		}

		expected := test.R

		// override expectation for semantic differences
		if test.CEL != nil {
			expected = test.CEL
		}

		if !reflect.DeepEqual(out, expected) {
			t.Fatalf("got %#v, expected %s (type %T and %T)", out, expected, out, expected)
		}

		// override referenced attributes value
		if test.ReferencedCEL != nil {
			test.Referenced = test.ReferencedCEL
		}

		if !test.CheckReferenced(b) {
			t.Errorf("check referenced attributes: got %v, expected %v", b.ReferencedList(), test.Referenced)
		}
	}
}

func TestCEXLCompatibility(t *testing.T) {
	for _, test := range ilt.TestData {
		if test.E == "" {
			continue
		}

		t.Run(test.TestName(), compatTest(test))
	}
}

func BenchmarkInterpreter(b *testing.B) {
	for _, test := range ilt.TestData {
		if !test.Bench {
			continue
		}

		finder := attribute.NewFinder(test.Conf())
		builder := NewBuilder(finder, LegacySyntaxCEL)
		ex, _, _ := builder.Compile(test.E)
		bg := ilt.NewFakeBag(test.I)

		b.Run(test.TestName(), func(bb *testing.B) {
			for i := 0; i < bb.N; i++ {
				_, _ = ex.Evaluate(bg)
			}
		})
	}
}

var accessLog = map[string]string{
	"connectionEvent":        `connection.event | ""`,
	"sourceIp":               `source.ip | ip("0.0.0.0")`,
	"sourceApp":              `source.labels["app"] | ""`,
	"sourcePrincipal":        `source.principal | ""`,
	"sourceName":             `source.name | ""`,
	"sourceWorkload":         `source.workload.name | ""`,
	"sourceNamespace":        `source.namespace | ""`,
	"sourceOwner":            `source.owner | ""`,
	"destinationApp":         `destination.labels["app"] | ""`,
	"destinationIp":          `destination.ip | ip("0.0.0.0")`,
	"destinationServiceHost": `destination.service.host | ""`,
	"destinationWorkload":    `destination.workload.name | ""`,
	"destinationName":        `destination.name | ""`,
	"destinationNamespace":   `destination.namespace | ""`,
	"destinationOwner":       `destination.owner | ""`,
	"destinationPrincipal":   `destination.principal | ""`,
	"protocol":               `context.protocol | "tcp"`,
	"connectionDuration":     `connection.duration | "0ms"`,
	// nolint: lll
	"connection_security_policy": `conditional((context.reporter.kind | "inbound") == "outbound", "unknown", conditional(connection.mtls | false, "mutual_tls", "none"))`,
	"requestedServerName":        `connection.requested_server_name | ""`,
	"receivedBytes":              `connection.received.bytes | 0`,
	"sentBytes":                  `connection.sent.bytes | 0`,
	"totalReceivedBytes":         `connection.received.bytes_total | 0`,
	"totalSentBytes":             `connection.sent.bytes_total | 0`,
	"reporter":                   `conditional((context.reporter.kind | "inbound") == "outbound", "source", "destination")`,
	"responseFlags":              `context.proxy_error_code | ""`,
}

var attributes = map[string]*v1beta1.AttributeManifest_AttributeInfo{
	"connection.event":                 {ValueType: v1beta1.STRING},
	"source.ip":                        {ValueType: v1beta1.IP_ADDRESS},
	"source.labels":                    {ValueType: v1beta1.STRING_MAP},
	"source.principal":                 {ValueType: v1beta1.STRING},
	"source.name":                      {ValueType: v1beta1.STRING},
	"source.workload.name":             {ValueType: v1beta1.STRING},
	"source.namespace":                 {ValueType: v1beta1.STRING},
	"source.owner":                     {ValueType: v1beta1.STRING},
	"destination.ip":                   {ValueType: v1beta1.IP_ADDRESS},
	"destination.labels":               {ValueType: v1beta1.STRING_MAP},
	"destination.principal":            {ValueType: v1beta1.STRING},
	"destination.name":                 {ValueType: v1beta1.STRING},
	"destination.workload.name":        {ValueType: v1beta1.STRING},
	"destination.namespace":            {ValueType: v1beta1.STRING},
	"destination.owner":                {ValueType: v1beta1.STRING},
	"destination.service.host":         {ValueType: v1beta1.STRING},
	"context.protocol":                 {ValueType: v1beta1.STRING},
	"connection.duration":              {ValueType: v1beta1.DURATION},
	"context.reporter.kind":            {ValueType: v1beta1.STRING},
	"context.proxy_error_code":         {ValueType: v1beta1.STRING},
	"connection.mtls":                  {ValueType: v1beta1.BOOL},
	"connection.requested_server_name": {ValueType: v1beta1.STRING},
	"connection.received.bytes":        {ValueType: v1beta1.INT64},
	"connection.sent.bytes":            {ValueType: v1beta1.INT64},
	"connection.received.bytes_total":  {ValueType: v1beta1.INT64},
	"connection.sent.bytes_total":      {ValueType: v1beta1.INT64},
}

func BenchmarkAccessLogCEL(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		finder := attribute.NewFinder(attributes)
		builder := NewBuilder(finder, LegacySyntaxCEL)
		for _, expr := range accessLog {
			_, _, err := builder.Compile(expr)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkAccessLogCEXL(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		finder := attribute.NewFinder(attributes)
		builder := compiled.NewBuilder(finder)
		for _, expr := range accessLog {
			_, _, err := builder.Compile(expr)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

type (
	testCase struct {
		text       string
		checkErr   string
		bag        map[string]interface{}
		result     interface{}
		referenced []string
	}
)

var (
	tests = []testCase{
		{
			text:   `2 + 2 == 4`,
			result: true,
		},
		{
			text:   `(1/0 == 0) || true`,
			result: true,
		},
		{
			text:   `(1/0 == 0) && false`,
			result: false,
		},
		{
			text: `test.bool ? 1/0 : 1`,
			bag: map[string]interface{}{
				"test.bool": true,
			},
			result:     errors.New("divide by zero"),
			referenced: []string{"test.bool"},
		},
		{
			text: `1 + 2 * request.size + size("test")`,
			bag: map[string]interface{}{
				"request.size": int64(5),
			},
			result:     int64(15),
			referenced: []string{"request.size"},
		},
		{
			text: `context.reporter.kind == "client"`,
			bag: map[string]interface{}{
				"context.reporter.kind": "client",
			},
			result:     true,
			referenced: []string{"context.reporter.kind"},
		},
		{
			text:       `abc`,
			result:     "",
			referenced: []string{"-abc"},
		},
		{
			text:       `abc`,
			bag:        map[string]interface{}{"abc": "test"},
			result:     "test",
			referenced: []string{"abc"},
		},
		{
			text:       `context.reporter.kind`,
			result:     "",
			referenced: []string{"-context.reporter.kind"},
		},
		{
			text:       `request.size`,
			result:     int64(0),
			referenced: []string{"-request.size"},
		},
		{
			text:       `test.double`,
			result:     float64(0),
			referenced: []string{"-test.double"},
		},
		{
			text:       `test.bool`,
			result:     false,
			referenced: []string{"-test.bool"},
		},
		{
			text:       `source.ip`,
			result:     []byte{},
			referenced: []string{"-source.ip"},
		},
		{
			text:     `context.report`,
			checkErr: "undefined field 'report'",
		},
		{
			text:     `request.headers[1]`,
			checkErr: `found no matching overload for '_[_]' applied to '(map(string, string), int)'`,
		},
		{
			text:     `context.reporter == "client"`,
			checkErr: `found no matching overload for '_==_' applied to '(.context.reporter, string)'`,
		},
		{
			text:     `context.reporter.kind == 2`,
			checkErr: `found no matching overload for '_==_' applied to '(string, int)'`,
		},
		{
			text: `source.ip == destination.ip`,
			bag: map[string]interface{}{
				"source.ip":      []byte{127, 0, 0, 1},
				"destination.ip": []byte(net.ParseIP("127.0.0.1")), // this has IPv6 byte slice instead
			},
			result:     true,
			referenced: []string{"destination.ip", "source.ip"},
		},
		{
			text:     `size(source.ip)`,
			checkErr: `found no matching overload for 'size' applied to '(istio.policy.v1beta1.IPAddress)'`,
		},
		{
			text: `conditional(context.reporter.kind == "client", "outbound", "inbound")`,
			bag: map[string]interface{}{
				"context.reporter.kind": "server",
			},
			result:     "inbound",
			referenced: []string{"context.reporter.kind"},
		},
		{
			text: `connection.duration + duration("30s") == duration("1m")`,
			bag: map[string]interface{}{
				"connection.duration": 30 * time.Second,
			},
			result:     true,
			referenced: []string{"connection.duration"},
		},
		{
			text: `timestamp("2017-01-15T01:30:15.01Z") + response.duration == timestamp("2017-01-15T01:31:15.01Z")`,
			bag: map[string]interface{}{
				"response.duration": 1 * time.Minute,
			},
			result:     true,
			referenced: []string{"response.duration"},
		},
		{
			text:   `timestamp("2017-01-15T01:30:15.01Z") > timestamp("2017-01-15T01:31:15.01Z")`,
			result: false,
		},
		{
			text: `request.time > context.time`,
			bag: map[string]interface{}{
				"request.time": time.Date(1999, time.December, 31, 23, 59, 0, 0, time.UTC),
				"context.time": time.Date(1977, time.February, 4, 12, 0, 0, 0, time.UTC),
			},
			result:     true,
			referenced: []string{"context.time", "request.time"},
		},
		{
			text:       `request.time + connection.duration == context.time`,
			result:     true,
			referenced: []string{"-connection.duration", "-context.time", "-request.time"},
		},
		{
			text:       `request.headers["x-user"] == '''john'''`,
			result:     errors.New("no such key: 'x-user'"),
			referenced: []string{"-request.headers"},
		},
		{
			text: `request.headers["x-user"] == '''john'''`,
			bag: map[string]interface{}{
				"request.headers": map[string]string{"x-user": "john"},
			},
			result:     true,
			referenced: []string{"request.headers", "request.headers[x-user]"},
		},
		{
			text: `request.headers["x-user"] == '''john'''`,
			bag: map[string]interface{}{
				"request.headers": map[string]string{"x-other-user": "juan"},
			},
			result:     errors.New("no such key: 'x-user'"),
			referenced: []string{"-request.headers[x-user]", "request.headers"},
		},
		{
			text: `'app' in source.labels`,
			bag: map[string]interface{}{
				"source.labels": map[string]string{"app": "mixer"},
			},
			result:     true,
			referenced: []string{"source.labels", "source.labels[app]"},
		},
		{
			text:       `source.labels == request.headers`,
			result:     errors.New("stringmap does not support equality"),
			referenced: []string{"-request.headers", "-source.labels"},
		},
		{
			text:       `type(source.labels)`,
			result:     errors.New("cannot convert stringmap to CEL types"),
			referenced: []string{"-source.labels"},
		},
		{
			text: `size(source.labels)`,
			bag: map[string]interface{}{
				"source.labels": map[string]string{"app": "mixer", "zone": "us"},
			},
			// TODO(kuat): note that to support size() we would need to store the lookup in the attribute tracker
			result:     errors.New("size not implemented on stringmaps"),
			referenced: []string{"source.labels"},
		},
		{
			text:   `email("user@istio.io")`,
			result: "user@istio.io",
		},
		{
			text:   `email("")`,
			result: errors.New("error converting '' to e-mail: 'mail: no address'"),
		},
		{
			text:   `dnsName("istio.io")`,
			result: "istio.io",
		},
		{
			text:   `dnsName("")`,
			result: errors.New("error converting '' to dns name: 'idna: invalid label \"\"'"),
		},
		{
			text:   `uri("http://istio.io")`,
			result: "http://istio.io",
		},
		{
			text:   `uri("")`,
			result: errors.New("error converting string to uri: empty string"),
		},
		{
			text:   `ip("127.0.0.1")`,
			result: []byte(net.ParseIP("127.0.0.1")),
		},
		{
			text:   `ip("blah")`,
			result: errors.New("could not convert blah to IP_ADDRESS"),
		},
		{
			text:   `"ab".startsWith("a")`,
			result: true,
		},
		{
			text:   `"ab".endsWith("a")`,
			result: false,
		},
		{
			text:   `"ab".reverse()`,
			result: "ba",
		},
		{
			text:   `reverse("ab")`,
			result: "ba",
		},
		{
			text:   `toLower("Ab")`,
			result: "ab",
		},
		{
			text:       `conditional(context.reporter.kind == "client", pick(abc, "test"), "inbound")`,
			result:     "inbound",
			referenced: []string{"-context.reporter.kind"},
		},
		{
			text:       `conditional(context.reporter.kind == "client", pick("outbound", "test"), "inbound")`,
			bag:        map[string]interface{}{"context.reporter.kind": "client"},
			result:     "outbound",
			referenced: []string{"context.reporter.kind"},
		},
		{
			text:       `pick(request.size, response.size, 100)`,
			result:     int64(100),
			referenced: []string{"-request.size", "-response.size"},
		},
		{
			text:       `pick(request.size, response.size, 100)`,
			bag:        map[string]interface{}{"request.size": int64(123)},
			result:     int64(123),
			referenced: []string{"request.size"},
		},
		{
			text:       `pick(request.size, response.size, 100)`,
			bag:        map[string]interface{}{"response.size": int64(345)},
			result:     int64(345),
			referenced: []string{"-request.size", "response.size"},
		},
		{
			text:       `"value=" + pick(source.name, "unknown")`,
			result:     "value=unknown",
			referenced: []string{"-source.name"},
		},
		{
			text:       `"value=" + pick(source.name, "unknown")`,
			bag:        map[string]interface{}{"source.name": "x"},
			result:     "value=x",
			referenced: []string{"source.name"},
		},
		{
			text:       `pick(source.name, request.headers["x-user"], "john")`,
			result:     "john",
			referenced: []string{"-request.headers", "-source.name"},
		},
		{
			text:       `pick(source.name, request.headers["x-user"], "john")`,
			bag:        map[string]interface{}{"source.name": "x"},
			result:     "x",
			referenced: []string{"source.name"},
		},
		{
			text:       `pick(source.name, request.headers["x-user"], "john")`,
			bag:        map[string]interface{}{"request.headers": map[string]string{}},
			result:     "john",
			referenced: []string{"-request.headers[x-user]", "-source.name", "request.headers"},
		},
		{
			text:       `pick(source.name, request.headers["x-user"], "john")`,
			bag:        map[string]interface{}{"request.headers": map[string]string{"x": "y"}},
			result:     "john",
			referenced: []string{"-request.headers[x-user]", "-source.name", "request.headers"},
		},
		{
			text:       `pick(source.name, request.headers["x-user"], "john")`,
			bag:        map[string]interface{}{"request.headers": map[string]string{"x-user": "y"}},
			result:     "y",
			referenced: []string{"-source.name", "request.headers", "request.headers[x-user]"},
		},
		{
			text:       `pick(source.labels["app"], source.name, "unknown")`,
			result:     "unknown",
			referenced: []string{"-source.labels", "-source.name"},
		},
		{
			text:       `pick(source.labels["app"], source.name, "unknown")`,
			bag:        map[string]interface{}{"source.name": "x"},
			result:     "x",
			referenced: []string{"-source.labels", "source.name"},
		},
		{
			text:       `pick(source.labels["app"], source.name, "unknown")`,
			bag:        map[string]interface{}{"source.labels": map[string]string{"app": "istio"}},
			result:     "istio",
			referenced: []string{"source.labels", "source.labels[app]"},
		},
		{
			text:   `has(context.reporter)`,
			result: true,
		},
		{
			text:       `has(context.reporter.kind)`,
			result:     false,
			referenced: []string{"-context.reporter.kind"},
		},
		{
			text:     `has(context.report)`,
			checkErr: "undefined field 'report'",
		},
		{
			text:     `request.size.length`,
			checkErr: `does not support field selection`,
		},
		{
			text:     `has(request.header.test)`,
			checkErr: "undefined field 'header'",
		},
		{
			text:       `has(request.headers.test)`,
			result:     false,
			referenced: []string{"-request.headers"},
		},
		{
			text:       `has(request.headers.test)`,
			bag:        map[string]interface{}{"request.headers": map[string]string{"test": "test"}},
			result:     true,
			referenced: []string{"request.headers", "request.headers[test]"},
		},
		{
			text:   `has({}.a)`,
			result: false,
		},
		{
			text:   `{}.a`,
			result: errors.New("no such key: a"),
		},
		{
			text:   `{'a':'b'}["a"]`,
			result: "b",
		},
		{
			text:   `{}`,
			result: attribute.WrapStringMap(nil),
		},
		{
			text:   `{'a':'b'}`,
			result: map[ref.Val]ref.Val{types.String("a"): types.String("b")},
		},
		{
			text:   `[1,2,3][0]`,
			result: int64(1),
		},
		{
			text:       `request.size + google.protobuf.Int64Value{value: 1}`,
			bag:        map[string]interface{}{"request.size": int64(123)},
			result:     int64(124),
			referenced: []string{"request.size"},
		},
		{
			text:   `type(context.reporter.kind) == string`,
			result: true,
			// note that type lookup is a runtime operation, so the attribute lookup is necessary
			referenced: []string{"-context.reporter.kind"},
		},
		//Taken from cel-go strings readme: https://github.com/google/cel-go/tree/master/ext
		//Replace
		{
			text:       `source.name.replace('he', 'we')`,
			bag:        map[string]interface{}{"source.name": "hello hello"},
			result:     "wello wello",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.replace('he', 'we', -1)`,
			bag:        map[string]interface{}{"source.name": "hello hello"},
			result:     "wello wello",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.replace('he', 'we', 1)`,
			bag:        map[string]interface{}{"source.name": "hello hello"},
			result:     "wello hello",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.replace('he', 'we', 0)`,
			bag:        map[string]interface{}{"source.name": "hello hello"},
			result:     "hello hello",
			referenced: []string{"source.name"},
		},
		//CharAt
		{
			text:       `source.name.charAt(4)`,
			bag:        map[string]interface{}{"source.name": "hello"},
			result:     "o",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.charAt(5)`,
			bag:        map[string]interface{}{"source.name": "hello"},
			result:     "",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.charAt(-1)`,
			bag:        map[string]interface{}{"source.name": "hello"},
			result:     errors.New("index out of range: -1"),
			referenced: []string{"source.name"},
		},
		//index of
		{
			text:       `source.name.indexOf('')`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(0),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.indexOf('ello')`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(1),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.indexOf('jello')`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(-1),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.indexOf('', 2)`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(2),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.indexOf('ello', 2)`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(7),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.indexOf('ello', 20)`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     errors.New("index out of range: 20"),
			referenced: []string{"source.name"},
		},
		//LastIndexOf
		{
			text:       `source.name.lastIndexOf('')`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(12),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.lastIndexOf('ello')`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(7),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.lastIndexOf('jello')`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(-1),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.lastIndexOf('ello', 6)`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     int64(1),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.lastIndexOf('ello', -1)`,
			bag:        map[string]interface{}{"source.name": "hello mellow"},
			result:     errors.New("index out of range: -1"),
			referenced: []string{"source.name"},
		},
		//Split
		{
			text:       `source.name.split(' ')`,
			bag:        map[string]interface{}{"source.name": "hello hello hello"},
			result:     []string{"hello", "hello", "hello"},
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.split(' ', 0)`,
			bag:        map[string]interface{}{"source.name": "hello hello hello"},
			result:     []string{},
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.split(' ', 1)`,
			bag:        map[string]interface{}{"source.name": "hello hello hello"},
			result:     []string{"hello hello hello"},
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.split(' ', 2)`,
			bag:        map[string]interface{}{"source.name": "hello hello hello"},
			result:     []string{"hello", "hello hello"},
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.split(' ', -1)`,
			bag:        map[string]interface{}{"source.name": "hello hello hello"},
			result:     []string{"hello", "hello", "hello"},
			referenced: []string{"source.name"},
		},
		//substring
		{
			text:       `source.name.substring(4)`,
			bag:        map[string]interface{}{"source.name": "tacocat"},
			result:     "cat",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.substring(0, 4)`,
			bag:        map[string]interface{}{"source.name": "tacocat"},
			result:     "taco",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.substring(-1)`,
			bag:        map[string]interface{}{"source.name": "tacocat"},
			result:     errors.New("index out of range: -1"),
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.substring(2, 1)`,
			bag:        map[string]interface{}{"source.name": "tacocat"},
			result:     errors.New("invalid substring range. start: 2, end: 1"),
			referenced: []string{"source.name"},
		},
		//trim
		{
			text:       `source.name.trim()`,
			bag:        map[string]interface{}{"source.name": "  foo  "},
			result:     "foo",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.trim()`,
			bag:        map[string]interface{}{"source.name": "foo \n"},
			result:     "foo",
			referenced: []string{"source.name"},
		},
		{
			text:       `source.name.trim()`,
			bag:        map[string]interface{}{"source.name": "foo"},
			result:     "foo",
			referenced: []string{"source.name"},
		},
	}
	attrs = map[string]*v1beta1.AttributeManifest_AttributeInfo{
		"abc":                       {ValueType: v1beta1.STRING},
		"connection.duration":       {ValueType: v1beta1.DURATION},
		"connection.id":             {ValueType: v1beta1.STRING},
		"connection.received.bytes": {ValueType: v1beta1.INT64},
		"connection.sent.bytes":     {ValueType: v1beta1.INT64},
		"context.protocol":          {ValueType: v1beta1.STRING},
		"context.time":              {ValueType: v1beta1.TIMESTAMP},
		"context.reporter.kind":     {ValueType: v1beta1.STRING},
		"destination.ip":            {ValueType: v1beta1.IP_ADDRESS},
		"destination.labels":        {ValueType: v1beta1.STRING_MAP},
		"destination.name":          {ValueType: v1beta1.STRING},
		"request.headers":           {ValueType: v1beta1.STRING_MAP},
		"request.host":              {ValueType: v1beta1.STRING},
		"request.size":              {ValueType: v1beta1.INT64},
		"request.time":              {ValueType: v1beta1.TIMESTAMP},
		"response.code":             {ValueType: v1beta1.INT64},
		"response.duration":         {ValueType: v1beta1.DURATION},
		"response.headers":          {ValueType: v1beta1.STRING_MAP},
		"response.size":             {ValueType: v1beta1.INT64},
		"response.time":             {ValueType: v1beta1.TIMESTAMP},
		"source.ip":                 {ValueType: v1beta1.IP_ADDRESS},
		"source.labels":             {ValueType: v1beta1.STRING_MAP},
		"source.name":               {ValueType: v1beta1.STRING},
		"test.bool":                 {ValueType: v1beta1.BOOL},
		"test.double":               {ValueType: v1beta1.DOUBLE},
		"test.i32":                  {ValueType: v1beta1.INT64},
		"test.i64":                  {ValueType: v1beta1.INT64},
		"test.float":                {ValueType: v1beta1.DOUBLE},
		"test.uri":                  {ValueType: v1beta1.URI},
		"test.dns_name":             {ValueType: v1beta1.DNS_NAME},
		"test.email_address":        {ValueType: v1beta1.EMAIL_ADDRESS},
	}
)

func testExpression(env *celgo.Env, provider *attributeProvider, test testCase, mutex sync.Locker) func(t *testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		// expressions must parse
		mutex.Lock()
		expr, iss := env.Parse(test.text)
		if iss != nil && iss.Err() != nil {
			mutex.Unlock()
			t.Fatalf("unexpected parsing error: %v", iss.Err())
		}
		t.Log(debug.ToDebugString(expr.Expr()))

		// expressions may fail type checking
		checked, iss := env.Check(expr)
		mutex.Unlock()
		if iss != nil {
			if test.checkErr == "" {
				t.Fatalf("unexpected check error: %v", iss)
			}
			if !strings.Contains(iss.String(), test.checkErr) {
				t.Fatalf("expected check error %q, got %v", test.checkErr, iss.String())
			}
			return
		} else if test.checkErr != "" {
			t.Fatalf("expected check error: %s", test.checkErr)
		}

		checkedExpr, _ := celgo.AstToCheckedExpr(checked)
		t.Log(checker.Print(expr.Expr(), checkedExpr))

		// expressions must evaluate to the right result
		eval, err := env.Program(checked, standardOverloads)
		if err != nil {
			t.Fatal(err)
		}
		b := ilt.NewFakeBag(test.bag)

		result, _, err := eval.Eval(provider.newActivation(b))

		var got interface{}
		if err != nil {
			got = err
		} else {
			got, err = recoverValue(result)
			if err != nil {
				got = err
			}
		}

		if !reflect.DeepEqual(got, test.result) {
			t.Errorf("expected result %v, got %v (%T and %T)", test.result, got, test.result, got)
		}

		// auto-fill referenced
		if test.referenced == nil {
			test.referenced = []string{}
		}
		if referenced := b.ReferencedList(); !reflect.DeepEqual(referenced, test.referenced) {
			t.Errorf("referenced attributes: got %v, want %v", referenced, test.referenced)
		}
	}
}

func TestCELExpressions(t *testing.T) {
	provider := newAttributeProvider(attrs)
	env := provider.newEnvironment()

	//TODO remove the mutex once CEL data race is fixed
	//ref: https://github.com/google/cel-go/issues/175
	mutex := &sync.Mutex{}

	for _, test := range tests {
		t.Run(test.text, testExpression(env, provider, test, mutex))
	}
}
