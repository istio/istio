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

package expr

import (
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"istio.io/istio/mixer/pkg/attribute"
)

func TestGoodEval(tt *testing.T) {
	t, _ := time.Parse(time.RFC3339, "2015-01-02T15:04:35Z")
	tests := []struct {
		src    string
		tmap   map[string]interface{}
		result interface{}
		err    string
	}{
		{
			"a == 2",
			map[string]interface{}{
				"a": int64(2),
			},
			true, "",
		},
		{
			"a != 2",
			map[string]interface{}{
				"a": int64(2),
			},
			false, "",
		},
		{
			"a != 2",
			map[string]interface{}{
				"d": int64(2),
			},
			false, "unresolved attribute",
		},
		{
			"2 != a",
			map[string]interface{}{
				"d": int64(2),
			},
			false, "unresolved attribute",
		},
		{
			"a ",
			map[string]interface{}{
				"a": int64(2),
			},
			int64(2), "",
		},
		{
			"true == a",
			map[string]interface{}{
				"a": int64(2),
			},
			false, "",
		},
		{
			"3.14 == a",
			map[string]interface{}{
				"a": int64(2),
			},
			false, "",
		},
		{
			"2 ",
			map[string]interface{}{
				"a": int64(2),
			},
			int64(2), "",
		},
		{
			`request.user == "user1"`,
			map[string]interface{}{
				"request.user": "user1",
			},
			true, "",
		},
		{
			`request.user2| request.user | "user1"`,
			map[string]interface{}{
				"request.user": "user2",
			},
			"user2", "",
		},
		{
			`request.user2| request.user3 | "user1"`,
			map[string]interface{}{
				"request.user": "user2",
			},
			"user1", "",
		},
		{
			`source.name| source.target`,
			map[string]interface{}{
				"source.name":   nil,
				"source.target": nil,
			},
			nil, "",
		},
		{
			`request.size| 200`,
			map[string]interface{}{
				"request.size": int64(120),
			},
			int64(120), "",
		},
		{
			`request.size| 200`,
			map[string]interface{}{
				"request.size": int64(0),
			},
			int64(0), "",
		},
		{
			`request.size| 200`,
			map[string]interface{}{
				"request.size1": int64(0),
			},
			int64(200), "",
		},
		{
			`(x == 20 && y == 10) || x == 30`,
			map[string]interface{}{
				"x": int64(20),
				"y": int64(10),
			},
			true, "",
		},
		{
			`x == 20 && y == 10`,
			map[string]interface{}{
				"a": int64(20),
				"b": int64(10),
			},
			false, "unresolved attribute",
		},
		{
			`service.name == "*.ns1.cluster" && service.user == "admin"`,
			map[string]interface{}{
				"service.name": "svc1.ns1.cluster",
				"service.user": "admin",
			},
			true, "",
		},
		{
			`( origin.name | "unknown" ) == "users"`,
			map[string]interface{}{},
			false, "",
		},
		{
			`( origin.name | "unknown" ) == "users"`,
			map[string]interface{}{
				"origin.name": "users",
			},
			true, "",
		},
		{
			`request.header["user"] | "unknown"`,
			map[string]interface{}{
				"request.header": map[string]string{
					"myheader": "bbb",
				},
			},
			"unknown", "",
		},
		{
			`origin.name | "users"`,
			map[string]interface{}{
				"origin.name": "",
			},
			"users", "",
		},
		{
			`origin.name | "users"`,
			map[string]interface{}{},
			"users", "",
		},
		{
			`(x/y) == 30`,
			map[string]interface{}{
				"x": int64(20),
				"y": int64(10),
			},
			false, "unknown function: QUO",
		},
		{
			`request.header["X-FORWARDED-HOST"] == "aaa"`,
			map[string]interface{}{
				"request.header": map[string]string{
					"X-FORWARDED-HOST": "bbb",
				},
			},
			false, "",
		},
		{
			`request.header["X-FORWARDED-HOST"] == "aaa"`,
			map[string]interface{}{
				"request.header1": map[string]string{
					"X-FORWARDED-HOST": "bbb",
				},
			},
			false, "unresolved attribute",
		},
		{
			`request.header[headername] == "aaa"`,
			map[string]interface{}{
				"request.header": map[string]string{
					"X-FORWARDED-HOST": "bbb",
				},
			},
			false, "unresolved attribute",
		},
		{
			`request.header[headername] == "aaa"`,
			map[string]interface{}{
				"request.header": map[string]string{
					"X-FORWARDED-HOST": "aaa",
				},
				"headername": "X-FORWARDED-HOST",
			},
			true, "",
		},
		{
			`match(service.name, "*.ns1.cluster")`,
			map[string]interface{}{
				"service.name": "svc1.ns1.cluster",
			},
			true, "",
		},
		{
			`match(service.name, "*.ns1.cluster")`,
			map[string]interface{}{
				"service.name": "svc1.ns2.cluster",
			},
			false, "",
		},
		{
			`match(service.name, "*.ns1.cluster")`,
			map[string]interface{}{
				"service.name": 20,
			},
			false, "input 'str' to 'match' func was not a string",
		},
		{
			`match(service.name, servicename)`,
			map[string]interface{}{
				"service.name1": "svc1.ns2.cluster",
			},
			false, "unresolved attribute",
		},
		{
			`match(service.name, servicename)`,
			map[string]interface{}{
				"service.name": "svc1.ns2.cluster",
			},
			false, "unresolved attribute",
		},
		{
			`match(service.name, 1)`,
			map[string]interface{}{
				"service.name": "svc1.ns2.cluster",
			},
			false, "input 'pattern' to 'match' func was not a string",
		},
		{
			`target.ip| ip("10.1.12.3")`,
			map[string]interface{}{
				"target.ip": "",
			},
			[]uint8(net.ParseIP("10.1.12.3")), "",
		},
		{
			`target.ip| ip(2)`,
			map[string]interface{}{
				"target.ip": "",
			},
			nil, "input to 'ip' func was not a string",
		},
		{
			`target.ip| ip("10.1.12")`,
			map[string]interface{}{
				"target.ip": "",
			},
			nil, "could not convert '10.1.12' to IP_ADDRESS",
		},
		{
			`request.time | timestamp("2015-01-02T15:04:35Z")`,
			map[string]interface{}{
				"request.time": "",
			},
			t, "",
		},
		{
			`request.time | timestamp(2)`,
			map[string]interface{}{
				"request.time": "",
			},
			nil, "input to 'timestamp' func was not a string",
		},
		{
			`request.time | timestamp("242233")`,
			map[string]interface{}{
				"request.time": "",
			},
			nil, "could not convert '242233' to TIMESTAMP. expected format: '" + time.RFC3339 + "'",
		},
	}

	for idx, tst := range tests {
		tt.Run(fmt.Sprintf("[%d] %s", idx, tst.src), func(t *testing.T) {
			attrs := &bag{attrs: tst.tmap}
			exp, err := Parse(tst.src)
			if err != nil {
				t.Errorf("[%d] unexpected error: %v", idx, err)
				return
			}
			res, err := exp.Eval(attrs, FuncMap())
			if err != nil {
				if tst.err == "" {
					t.Errorf("[%d] unexpected error: %v", idx, err)
				} else if !strings.Contains(err.Error(), tst.err) {
					t.Errorf("[%d] got <%q>\nwant <%q>", idx, err, tst.err)
				}
				return
			}
			if strings.Contains(tst.src, "ip") {
				if !reflect.DeepEqual(res, tst.result) {
					t.Errorf("[%d] %s got <%q>\nwant <%q>", idx, exp.String(), res, tst.result)
				}
			} else {
				if res != tst.result {
					t.Errorf("[%d] %s got <%q>\nwant <%q>", idx, exp.String(), res, tst.result)
				}
			}
		})
	}

}

func TestCEXLEval(tt *testing.T) {
	success := "_SUCCESS_"
	type mtype int
	const (
		anyType mtype = iota
		boolType
		stringType
	)
	tests := []struct {
		src    string
		tmap   map[string]interface{}
		result interface{}
		err    string
		fn     mtype
	}{
		{
			"a = 2",
			map[string]interface{}{
				"a": int64(2),
			},
			true, "unable to parse", anyType,
		},
		{
			"a == 2",
			map[string]interface{}{
				"a": int64(2),
			},
			true, success, anyType,
		},
		{
			"a == 3",
			map[string]interface{}{
				"a": int64(2),
			},
			false, success, anyType,
		},
		{
			"a == 2",
			map[string]interface{}{
				"a": int64(2),
			},
			true, success, boolType,
		},
		{
			"a",
			map[string]interface{}{
				"a": int64(2),
			},
			true, "typeError", boolType,
		},
		{
			"a == 2",
			map[string]interface{}{},
			true, "unresolved attribute", boolType,
		},
		{
			`request.user | "user1"`,
			map[string]interface{}{},
			"user1", success, stringType,
		},
		{
			"a",
			map[string]interface{}{
				"a": int64(2),
			},
			true, "typeError", stringType,
		},
		{
			"a == 2",
			map[string]interface{}{},
			true, "unresolved attribute", stringType,
		},
	}
	ev, er := NewCEXLEvaluator(DefaultCacheSize)
	if er != nil {
		tt.Errorf("Failed to create expression evaluator: %v", er)
	}
	var ret interface{}
	var err error
	for idx, tst := range tests {
		tt.Run(fmt.Sprintf("[%d] %s", idx, tst.src), func(t *testing.T) {
			attrs := &bag{attrs: tst.tmap}
			switch tst.fn {
			case anyType:
				ret, err = ev.Eval(tst.src, attrs)
			case boolType:
				ret, err = ev.EvalPredicate(tst.src, attrs)
			case stringType:
				ret, err = ev.EvalString(tst.src, attrs)
			}
			if (err == nil) != (tst.err == success) {
				t.Errorf("[%d] got %s, want %s", idx, err, tst.err)
				return
			}
			// check if error is of the correct type
			if err != nil {
				if !strings.Contains(err.Error(), tst.err) {
					t.Errorf("[%d] got %s, want %s", idx, err, tst.err)
				}
				return
			}
			// check result
			if ret != tst.result {
				t.Errorf("[%d] got %s, want %s", idx, ret, tst.result)
			}
		})
	}

}

// fake bag
type bag struct {
	attribute.Bag
	attrs map[string]interface{}
}

func (b *bag) Get(name string) (interface{}, bool) {
	c, found := b.attrs[name]
	return c, found
}

func (b *bag) Names() []string {
	return []string{}
}

func (b *bag) Done() {
}
