// Copyright 2018 Istio Authors
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
	"testing"
	"time"

	"github.com/google/cel-go/common/debug"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	ilt "istio.io/istio/mixer/pkg/il/testing"
)

var (
	tests = []struct {
		text       string
		checkErr   string
		bag        map[string]interface{}
		result     interface{}
		referenced []string
	}{
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
			text:       `as`,
			result:     "",
			referenced: []string{"-as"},
		},
		{
			text:       `as`,
			bag:        map[string]interface{}{"as": "test"},
			result:     "test",
			referenced: []string{"as"},
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
				"context.time": time.Date(1977, time.February, 4, 12, 00, 0, 0, time.UTC),
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
			text:       `conditional(context.reporter.kind == "client", getOrElse(as, "test"), "inbound")`,
			result:     "inbound",
			referenced: []string{"-context.reporter.kind"},
		},
		{
			text:       `conditional(context.reporter.kind == "client", getOrElse("outbound", "test"), "inbound")`,
			bag:        map[string]interface{}{"context.reporter.kind": "client"},
			result:     "outbound",
			referenced: []string{"context.reporter.kind"},
		},
		{
			text:       `getOrElse(request.size, response.size, 100)`,
			result:     int64(100),
			referenced: []string{"-request.size", "-response.size"},
		},
		{
			text:       `getOrElse(request.size, response.size, 100)`,
			bag:        map[string]interface{}{"request.size": int64(123)},
			result:     int64(123),
			referenced: []string{"request.size"},
		},
		{
			text:       `getOrElse(request.size, response.size, 100)`,
			bag:        map[string]interface{}{"response.size": int64(345)},
			result:     int64(345),
			referenced: []string{"-request.size", "response.size"},
		},
		{
			text:       `"value=" + getOrElse(source.name, "unknown")`,
			result:     "value=unknown",
			referenced: []string{"-source.name"},
		},
		{
			text:       `"value=" + getOrElse(source.name, "unknown")`,
			bag:        map[string]interface{}{"source.name": "x"},
			result:     "value=x",
			referenced: []string{"source.name"},
		},
		{
			text:       `getOrElse(source.name, request.headers["x-user"], "john")`,
			result:     "john",
			referenced: []string{"-request.headers", "-source.name"},
		},
		{
			text:       `getOrElse(source.name, request.headers["x-user"], "john")`,
			bag:        map[string]interface{}{"source.name": "x"},
			result:     "x",
			referenced: []string{"source.name"},
		},
		{
			text:       `getOrElse(source.name, request.headers["x-user"], "john")`,
			bag:        map[string]interface{}{"request.headers": map[string]string{}},
			result:     "john",
			referenced: []string{"-request.headers[x-user]", "-source.name", "request.headers"},
		},
		{
			text:       `getOrElse(source.name, request.headers["x-user"], "john")`,
			bag:        map[string]interface{}{"request.headers": map[string]string{"x": "y"}},
			result:     "john",
			referenced: []string{"-request.headers[x-user]", "-source.name", "request.headers"},
		},
		{
			text:       `getOrElse(source.name, request.headers["x-user"], "john")`,
			bag:        map[string]interface{}{"request.headers": map[string]string{"x-user": "y"}},
			result:     "y",
			referenced: []string{"-source.name", "request.headers", "request.headers[x-user]"},
		},
		{
			text:       `getOrElse(source.labels["app"], source.name, "unknown")`,
			result:     "unknown",
			referenced: []string{"-source.labels", "-source.name"},
		},
		{
			text:       `getOrElse(source.labels["app"], source.name, "unknown")`,
			bag:        map[string]interface{}{"source.name": "x"},
			result:     "x",
			referenced: []string{"-source.labels", "source.name"},
		},
		{
			text:       `getOrElse(source.labels["app"], source.name, "unknown")`,
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
			result: errors.New("no such key: 'a'"),
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
			result: map[ref.Value]ref.Value{types.String("a"): types.String("b")},
		},
		{
			text:   `[1,2,3][0]`,
			result: int64(1),
		},
		{
			text:       `request.size + google.protobuf.Int64Value{value: 1}.value`,
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
	}
	attrs = map[string]*v1beta1.AttributeManifest_AttributeInfo{
		"as":                        {ValueType: v1beta1.STRING},
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

func TestCELExpressions(t *testing.T) {
	provider := newAttributeProvider(attrs)
	env := provider.newEnvironment()
	i := provider.newInterpreter()
	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			// expressions must parse
			expr, err := Parse(test.text)
			if err != nil {
				t.Fatalf("unexpected parsing error: %v", err)
			}
			t.Log(debug.ToDebugString(expr))

			// expressions may fail type checking
			checked, err := Check(expr, env)
			if err != nil {
				if test.checkErr == "" {
					t.Fatalf("unexpected check error: %v", err)
				}
				if !strings.Contains(err.Error(), test.checkErr) {
					t.Fatalf("expected check error %q, got %v", test.checkErr, err)
				}
				return
			} else if test.checkErr != "" {
				t.Fatalf("expected check error: %s", test.checkErr)
			}

			// expressions must evaluate to the right result
			program := interpreter.NewCheckedProgram(checked)
			eval := i.NewInterpretable(program)
			b := ilt.NewFakeBag(test.bag)

			result, _ := eval.Eval(provider.newActivation(b))
			got, err := recoverValue(result)
			if err != nil {
				got = err
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
		})
	}
}
