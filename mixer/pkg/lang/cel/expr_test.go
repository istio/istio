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
	"github.com/google/cel-go/interpreter"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
)

var (
	tests = []struct {
		text     string
		checkErr string
		bag      map[string]interface{}
		result   interface{}
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
			result: errors.New("divide by zero"),
		},
		{
			text: `1 + 2 * request.size + size("test")`,
			bag: map[string]interface{}{
				"request.size": int64(5),
			},
			result: int64(15),
		},
		{
			text: `context.reporter.kind == "client"`,
			bag: map[string]interface{}{
				"context.reporter.kind": "client",
			},
			result: true,
		},
		{
			text:   `as`,
			result: "",
		},
		{
			text:   `as`,
			bag:    map[string]interface{}{"as": "test"},
			result: "test",
		},
		{
			text:   `context.reporter.kind`,
			result: "",
		},
		{
			text:   `request.size`,
			result: int64(0),
		},
		{
			text:   `test.double`,
			result: float64(0),
		},
		{
			text:   `test.bool`,
			result: false,
		},
		{
			text:   `source.ip`,
			result: []byte(nil),
		},
		{
			text:     `context.report`,
			checkErr: "undefined field 'report'",
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
				"destination.ip": []byte(net.ParseIP("127.0.0.1")),
			},
			result: true,
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
			result: "inbound",
		},
		{
			text: `connection.duration + duration("30s") == duration("1m")`,
			bag: map[string]interface{}{
				"connection.duration": 30 * time.Second,
			},
			result: true,
		},
		{
			text: `timestamp("2017-01-15T01:30:15.01Z") + response.duration == timestamp("2017-01-15T01:31:15.01Z")`,
			bag: map[string]interface{}{
				"response.duration": 1 * time.Minute,
			},
			result: true,
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
			result: true,
		},
		{
			text:   `request.time + connection.duration == context.time`,
			result: true,
		},
		{
			text:   `request.headers["x-user"] == '''john'''`,
			result: errors.New("no such key: 'x-user'"),
		},
		{
			text: `request.headers["x-user"] == '''john'''`,
			bag: map[string]interface{}{
				"request.headers": attribute.WrapStringMap(map[string]string{"x-user": "john"}),
			},
			result: true,
		},
		{
			text: `request.headers["x-user"] == '''john'''`,
			bag: map[string]interface{}{
				"request.headers": attribute.WrapStringMap(map[string]string{"x-other-user": "juan"}),
			},
			result: errors.New("no such key: 'x-user'"),
		},
		{
			text: `'app' in source.labels`,
			bag: map[string]interface{}{
				"source.labels": attribute.WrapStringMap(map[string]string{"app": "mixer"}),
			},
			result: true,
		},
		{
			text:   `source.labels == request.headers`,
			result: errors.New("stringmap does not support equality"),
		},
		{
			text:   `type(source.labels)`,
			result: errors.New("cannot convert stringmap to CEL types"),
		},
		{
			text: `size(source.labels)`,
			bag: map[string]interface{}{
				"source.labels": attribute.WrapStringMap(map[string]string{"app": "mixer", "zone": "us"}),
			},
			result: errors.New("size not implemented on stringmaps"),
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
		/*
			{
				text: `conditional(context.reporter.kind == "client", getOrElse("outbound", "test"), "inbound")`,
			},
			{
				text: `getOrElse(request.size, response.size, 100)`,
			},
			{
				text: `getOrElse(source.name, "unknown")`,
			},
			{
				text: `getOrElse(source.name, request.headers["x-user"], "john")`,
			},
			{
				text: `getOrElse(source.labels["app"], source.name, "unknown")`,
			},
			{
				text: `getOrElse(request.size, 200)`,
			},
		*/
	}
	// TODO: map reference tracking, attribute bag tracking, in for maps with references
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
			result, _ := eval.Eval(provider.newActivation(attribute.GetMutableBagForTesting(test.bag)))
			if !reflect.DeepEqual(result.Value(), test.result) {
				t.Fatalf("expected result %v, got %v", test.result, result.Value())
			}
		})
	}
}
