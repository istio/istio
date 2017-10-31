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

package ilt

import (
	"net"
	"time"

	pbv "istio.io/api/mixer/v1/config/descriptor"
	pb "istio.io/istio/mixer/pkg/config/proto"
)

var t, _ = time.Parse(time.RFC3339, "2015-01-02T15:04:35Z")

var TestData = []TestInfo{

	// Tests from expr/eval_test.go TestGoodEval
	{
		E: `a == 2`,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `a != 2`,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    false,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `a != 2`,
		I: map[string]interface{}{
			"d": int64(2),
		},
		Err:    "lookup failed: 'a'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: "2 != a",
		I: map[string]interface{}{
			"d": int64(2),
		},
		Err:    "lookup failed: 'a'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: "a ",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    int64(2),
		Conf: TestConfigs["Expr/Eval"],
	},

	// Compilation Error due to type mismatch
	{
		E: "true == a",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:          false,
		CompileErr: "EQ(true, $a) arg 2 ($a) typeError got INT64, expected BOOL",
		Conf:       TestConfigs["Expr/Eval"],
	},

	// Compilation Error due to type mismatch
	{
		E: "3.14 == a",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:          false,
		CompileErr: "EQ(3.14, $a) arg 2 ($a) typeError got INT64, expected DOUBLE",
		Conf:       TestConfigs["Expr/Eval"],
	},

	{
		E: "2 ",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    int64(2),
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.user == "user1"`,
		I: map[string]interface{}{
			"request.user": "user1",
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.user2| request.user | "user1"`,
		I: map[string]interface{}{
			"request.user": "user2",
		},
		R:    "user2",
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.user2| request.user3 | "user1"`,
		I: map[string]interface{}{
			"request.user": "user2",
		},
		R:    "user1",
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.size| 200`,
		I: map[string]interface{}{
			"request.size": int64(120),
		},
		R:    int64(120),
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.size| 200`,
		I: map[string]interface{}{
			"request.size": int64(0),
		},
		R:    int64(0),
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.size| 200`,
		I: map[string]interface{}{
			"request.size1": int64(0),
		},
		R:    int64(200),
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `(x == 20 && y == 10) || x == 30`,
		I: map[string]interface{}{
			"x": int64(20),
			"y": int64(10),
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `x == 20 && y == 10`,
		I: map[string]interface{}{
			"a": int64(20),
			"b": int64(10),
		},
		Err:    "lookup failed: 'x'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: `match(service.name, "*.ns1.cluster") && service.user == "admin"`,
		I: map[string]interface{}{
			"service.name": "svc1.ns1.cluster",
			"service.user": "admin",
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `( origin.name | "unknown" ) == "users"`,
		I:    map[string]interface{}{},
		R:    false,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `( origin.name | "unknown" ) == "users"`,
		I: map[string]interface{}{
			"origin.name": "users",
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.header["user"] | "unknown"`,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"myheader": "bbb",
			},
		},
		R:    "unknown",
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `origin.name | "users"`,
		I:    map[string]interface{}{},
		R:    "users",
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `(x/y) == 30`,
		I: map[string]interface{}{
			"x": int64(20),
			"y": int64(10),
		},
		CompileErr: "unknown function: QUO",
		AstErr:     "unknown function: QUO",
		Conf:       TestConfigs["Expr/Eval"],
	},
	{
		E: `request.header["X-FORWARDED-HOST"] == "aaa"`,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"X-FORWARDED-HOST": "bbb",
			},
		},
		R:    false,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.header["X-FORWARDED-HOST"] == "aaa"`,
		I: map[string]interface{}{
			"request.header1": map[string]string{
				"X-FORWARDED-HOST": "bbb",
			},
		},
		Err:    "lookup failed: 'request.header'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: `request.header[headername] == "aaa"`,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"X-FORWARDED-HOST": "bbb",
			},
		},
		Err:    "lookup failed: 'headername'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: `request.header[headername] == "aaa"`,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"X-FORWARDED-HOST": "aaa",
			},
			"headername": "X-FORWARDED-HOST",
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `match(service.name, "*.ns1.cluster")`,
		I: map[string]interface{}{
			"service.name": "svc1.ns1.cluster",
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `match(service.name, "*.ns1.cluster")`,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		R:    false,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `match(service.name, "*.ns1.cluster")`,
		I: map[string]interface{}{
			"service.name": 20,
		},
		Err:    "error converting value to string: '20'", // runtime error
		AstErr: "input 'str' to 'match' func was not a string",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: `match(service.name, servicename)`,
		I: map[string]interface{}{
			"service.name1": "svc1.ns2.cluster",
			"servicename":   "*.aaa",
		},
		Err:    "lookup failed: 'service.name'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: `match(service.name, servicename)`,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		Err:    "lookup failed: 'servicename'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: `match(service.name, 1)`,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		CompileErr: "match($service.name, 1) arg 2 (1) typeError got INT64, expected STRING",
		AstErr:     "input 'pattern' to 'match' func was not a string",
		Conf:       TestConfigs["Expr/Eval"],
	},
	{
		E:    `target.ip| ip("10.1.12.3")`,
		I:    map[string]interface{}{},
		R:    []uint8(net.ParseIP("10.1.12.3")),
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `target.ip| ip(2)`,
		I: map[string]interface{}{
			"target.ip": "",
		},
		CompileErr: "ip(2) arg 1 (2) typeError got INT64, expected STRING",
		AstErr:     "input to 'ip' func was not a string",
		Conf:       TestConfigs["Expr/Eval"],
	},
	{
		E:      `target.ip| ip("10.1.12")`,
		I:      map[string]interface{}{},
		Err:    "could not convert 10.1.12 to IP_ADDRESS",
		AstErr: "could not convert '10.1.12' to IP_ADDRESS",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.time | timestamp("2015-01-02T15:04:35Z")`,
		I:    map[string]interface{}{},
		R:    t,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `request.time | timestamp(2)`,
		I: map[string]interface{}{
			"request.time": "",
		},
		CompileErr: "timestamp(2) arg 1 (2) typeError got INT64, expected STRING",
		AstErr:     "input to 'timestamp' func was not a string",
		Conf:       TestConfigs["Expr/Eval"],
	},
	{
		E:      `request.time | timestamp("242233")`,
		I:      map[string]interface{}{},
		Err:    "could not convert '242233' to TIMESTAMP. expected format: '" + time.RFC3339 + "'",
		AstErr: "could not convert '242233' to TIMESTAMP. expected format: '" + time.RFC3339 + "'",
		Conf:   TestConfigs["Expr/Eval"],
	},

	// Tests from expr/eval_test.go TestCEXLEval
	{
		E: "a = 2",
		I: map[string]interface{}{
			"a": int64(2),
		},
		CompileErr: "unable to parse expression 'a = 2': 1:3: expected '==', found '='",
		AstErr:     "unable to parse",
		Conf:       TestConfigs["Expr/Eval"],
	},
	{
		E: "a == 2",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: "a == 3",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    false,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E: "a == 2",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    true,
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E:      "a == 2",
		I:      map[string]interface{}{},
		Err:    "lookup failed: 'a'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.user | "user1"`,
		I:    map[string]interface{}{},
		R:    "user1",
		Conf: TestConfigs["Expr/Eval"],
	},
	{
		E:      "a == 2",
		I:      map[string]interface{}{},
		Err:    "lookup failed: 'a'",
		AstErr: "unresolved attribute",
		Conf:   TestConfigs["Expr/Eval"],
	},
}

// TestInfo is a structure that contains detailed test information. Depending
// on the test type, various fields of the TestInfo struct will be used for
// testing purposes. For example, compiler can use E and IL to test
// expression => IL conversion, interpreter can use IL and I, R&Err for evaluation
// tests and the evaluator can use expression and I, R&Err to test evaluation.
type TestInfo struct {
	// E contains the expression that is being tested.
	E string

	// IL contains the textual IL representation of code.
	IL string

	// I contains the attribute bag used for testing.
	I map[string]interface{}

	// R contains the expected result of a successful evaluation.
	R interface{}

	// Err contains the expected error message of a failed evaluation.
	Err string

	// AstErr contains the expected error message of a failed evaluation, during AST evaluation.
	AstErr string

	// CompileErr contains the expected error message for a failed compilation.
	CompileErr string

	// Config field holds the GlobalConfig to use when compiling/evaluating the tests.
	// If nil, then "Default" config will be used.
	Conf *pb.GlobalConfig

	// SkipAst indicates that AST based evaluator should not be used for this test.
	SkipAst bool
}

// TestConfigs uses a standard set of configs to use when executing tests.
var TestConfigs map[string]*pb.GlobalConfig = map[string]*pb.GlobalConfig{
	"Expr/Eval": {
		Manifests: []*pb.AttributeManifest{
			{
				Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
					"a": {
						ValueType: pbv.INT64,
					},
					"request.user": {
						ValueType: pbv.STRING,
					},
					"request.user2": {
						ValueType: pbv.STRING,
					},
					"request.user3": {
						ValueType: pbv.STRING,
					},
					"source.name": {
						ValueType: pbv.STRING,
					},
					"source.target": {
						ValueType: pbv.STRING,
					},
					"request.size": {
						ValueType: pbv.INT64,
					},
					"request.size1": {
						ValueType: pbv.INT64,
					},
					"x": {
						ValueType: pbv.INT64,
					},
					"y": {
						ValueType: pbv.INT64,
					},
					"service.name": {
						ValueType: pbv.STRING,
					},
					"service.user": {
						ValueType: pbv.STRING,
					},
					"origin.name": {
						ValueType: pbv.STRING,
					},
					"request.header": {
						ValueType: pbv.STRING_MAP,
					},
					"request.time": {
						ValueType: pbv.TIMESTAMP,
					},
					"headername": {
						ValueType: pbv.STRING,
					},
					"target.ip": {
						ValueType: pbv.IP_ADDRESS,
					},
					"servicename": {
						ValueType: pbv.STRING,
					},
				},
			},
		},
	},
	"Default": {
		Manifests: []*pb.AttributeManifest{
			{
				Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
					"ai": {
						ValueType: pbv.INT64,
					},
					"ab": {
						ValueType: pbv.BOOL,
					},
					"as": {
						ValueType: pbv.STRING,
					},
					"ad": {
						ValueType: pbv.DOUBLE,
					},
					"ar": {
						ValueType: pbv.STRING_MAP,
					},
					"adur": {
						ValueType: pbv.DURATION,
					},
					"at": {
						ValueType: pbv.TIMESTAMP,
					},
					"aip": {
						ValueType: pbv.IP_ADDRESS,
					},
					"bi": {
						ValueType: pbv.INT64,
					},
					"bb": {
						ValueType: pbv.BOOL,
					},
					"bs": {
						ValueType: pbv.STRING,
					},
					"bd": {
						ValueType: pbv.DOUBLE,
					},
					"br": {
						ValueType: pbv.STRING_MAP,
					},
					"bdur": {
						ValueType: pbv.DURATION,
					},
					"bt": {
						ValueType: pbv.TIMESTAMP,
					},
					"t1": {
						ValueType: pbv.TIMESTAMP,
					},
					"t2": {
						ValueType: pbv.TIMESTAMP,
					},
					"bip": {
						ValueType: pbv.IP_ADDRESS,
					},
					"b1": {
						ValueType: pbv.BOOL,
					},
					"b2": {
						ValueType: pbv.BOOL,
					},
					"sm": {
						ValueType: pbv.STRING_MAP,
					},
				},
			},
		},
	},
}
