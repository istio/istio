// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copyAttrs of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sample

import (
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/proto"

	pb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/lang/compiled"
	sample_apa "istio.io/istio/mixer/template/sample/apa"
	sample_check "istio.io/istio/mixer/template/sample/check"
	sample_quota "istio.io/istio/mixer/template/sample/quota"
	sample_report "istio.io/istio/mixer/template/sample/report"
	"istio.io/pkg/attribute"
)

// struct for declaring a test case for testing CreateInstance calls.
type createInstanceTest struct {
	// name of the test
	name string

	// the name of the template to use
	template string

	// the instance parameter to pass-in for creating a builder
	param proto.Message

	// the attribute set to use
	attrs map[string]interface{}

	// the expected instance after evaluation
	expect interface{}

	// the expected error during CreateInstanceBuilder call, if any
	expectCreateError string

	// the expected error during build call, if any
	expectBuildError string
}

// default set of known attributes
var defaultAttributeInfos = map[string]*pb.AttributeManifest_AttributeInfo{
	"ai": {
		ValueType: pb.INT64,
	},
	"ai1": {
		ValueType: pb.INT64,
	},
	"ai2": {
		ValueType: pb.INT64,
	},
	"ai3": {
		ValueType: pb.INT64,
	},
	"ai4": {
		ValueType: pb.INT64,
	},
	"ai5": {
		ValueType: pb.INT64,
	},
	"ai6": {
		ValueType: pb.INT64,
	},
	"ai7": {
		ValueType: pb.INT64,
	},
	"ad": {
		ValueType: pb.DOUBLE,
	},
	"ab": {
		ValueType: pb.BOOL,
	},
	"as": {
		ValueType: pb.STRING,
	},
	"as1": {
		ValueType: pb.STRING,
	},
	"as2": {
		ValueType: pb.STRING,
	},
	"as3": {
		ValueType: pb.STRING,
	},
	"as4": {
		ValueType: pb.STRING,
	},
	"as5": {
		ValueType: pb.STRING,
	},
	"as6": {
		ValueType: pb.STRING,
	},
	"as7": {
		ValueType: pb.STRING,
	},
	"ats": {
		ValueType: pb.TIMESTAMP,
	},
	"ats2": {
		ValueType: pb.TIMESTAMP,
	},
	"adr": {
		ValueType: pb.DURATION,
	},
	"ap1": {
		ValueType: pb.IP_ADDRESS,
	},
	"generated.ai": {
		ValueType: pb.INT64,
	},
	"generated.ab": {
		ValueType: pb.BOOL,
	},
	"generated.ad": {
		ValueType: pb.DOUBLE,
	},
	"generated.as": {
		ValueType: pb.STRING,
	},
	"generated.ats": {
		ValueType: pb.TIMESTAMP,
	},
	"generated.adr": {
		ValueType: pb.DURATION,
	},
	"generated.email": {
		ValueType: pb.EMAIL_ADDRESS,
	},
	"generated.ip": {
		ValueType: pb.IP_ADDRESS,
	},
	"generated.labels": {
		ValueType: pb.STRING_MAP,
	},
}

func combineManifests(
	base map[string]*pb.AttributeManifest_AttributeInfo,
	manifests ...*pb.AttributeManifest) map[string]*pb.AttributeManifest_AttributeInfo {
	result := make(map[string]*pb.AttributeManifest_AttributeInfo)
	for k, v := range base {
		result[k] = v
	}

	for _, manifest := range manifests {
		for k, v := range manifest.Attributes {
			result[k] = v
		}
	}

	return result
}

func copyAttrs(attrs map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range attrs {
		result[k] = v
	}
	return result
}

func TestCreateInstanceBuilder(t *testing.T) {
	var tests []createInstanceTest
	tests = append(tests, generateReportTests()...)
	tests = append(tests, generateCheckTests()...)
	tests = append(tests, generateQuotaTests()...)
	tests = append(tests, generateApaTests()...)

	for _, tst := range tests {
		t.Run(tst.name, func(tt *testing.T) {
			expb := compiled.NewBuilder(attribute.NewFinder(defaultAttributeInfos))
			builder, e := SupportedTmplInfo[tst.template].CreateInstanceBuilder("instance1", tst.param, expb)
			assertErr(tt, "CreateInstanceBuilder", tst.expectCreateError, e)
			if tst.expectCreateError != "" {
				return
			}

			bag := attribute.GetMutableBagForTesting(tst.attrs)
			actual, e2 := builder(bag)
			assertErr(tt, "CreateInstanceBuilder", tst.expectBuildError, e2)
			if tst.expectBuildError != "" {
				return
			}

			if !reflect.DeepEqual(actual, tst.expect) {
				tt.Fatalf("Instance mismatch,\ngot =%+v\nwant=%+v", spew.Sdump(actual), spew.Sdump(tst.expect))
			}
		})
	}
}

var defaultApaAttributes = map[string]interface{}{
	"ats": time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
	"adr": 10 * time.Second,
	"as":  "val as",
	"as1": "val as1",
	"as2": "val as2",
	"as3": "val as3",
	"as4": "val as4",
	"as5": "val as5",
	"as6": "val as6",
	"ai":  int64(100),
	"ai2": int64(200),
	"ai3": int64(300),
	"ai4": int64(400),
	"ai5": int64(500),
	"ai6": int64(600),
	"ai7": int64(700),
	"ad":  float64(42.42),
	"ab":  true,
}

// a default Quota Instance Parameter. All of the fields can be calculated, given the values in the
// default bag.
var defaultApaInstanceParam = sample_apa.InstanceParam{
	StringPrimitive:                "as",
	Int64Primitive:                 "ai",
	TimeStamp:                      "ats",
	DoublePrimitive:                "ad",
	BoolPrimitive:                  "ab",
	Duration:                       "adr",
	Email:                          `"email@email"`,
	OptionalIP:                     `ip("0.0.0.0")`,
	DimensionsFixedInt64ValueDType: map[string]string{"ai2": "ai2"},
	Res3Map: map[string]*sample_apa.Resource3InstanceParam{
		"r3": {
			StringPrimitive:                "as2",
			Duration:                       "adr",
			TimeStamp:                      "ats",
			BoolPrimitive:                  "ab",
			DoublePrimitive:                "ad",
			Int64Primitive:                 "ai3",
			DimensionsFixedInt64ValueDType: map[string]string{"ai4": "ai4"},
		},
	},
	// These are not directly used by the tests in this file, but used by dispatcher tests.
	AttributeBindings: map[string]string{
		"generated.ai":     "$out.int64Primitive",
		"generated.ab":     "$out.boolPrimitive",
		"generated.ad":     "$out.doublePrimitive",
		"generated.as":     "$out.stringPrimitive",
		"generated.ats":    "$out.timeStamp",
		"generated.adr":    "$out.duration",
		"generated.email":  "$out.email",
		"generated.ip":     "$out.out_ip",
		"generated.labels": "$out.out_str_map",
	},
}

// given the default instance param and the default attributes, this is the expected quota instance. The name
// "instance1" is hard-coded.
var defaultApaInstance = &sample_apa.Instance{
	Name:                           "instance1",
	StringPrimitive:                "val as",
	Int64Primitive:                 int64(100),
	TimeStamp:                      time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
	Duration:                       10 * time.Second,
	BoolPrimitive:                  true,
	DoublePrimitive:                float64(42.42),
	Email:                          "email@email",
	OptionalIP:                     net.IPv4(0, 0, 0, 0),
	DimensionsFixedInt64ValueDType: map[string]int64{"ai2": int64(200)},
	Res3Map: map[string]*sample_apa.Resource3{
		"r3": {
			StringPrimitive:                "val as2",
			TimeStamp:                      time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
			Duration:                       10 * time.Second,
			DoublePrimitive:                float64(42.42),
			BoolPrimitive:                  true,
			Int64Primitive:                 int64(300),
			DimensionsFixedInt64ValueDType: map[string]int64{"ai4": int64(400)},
		},
	},
}

func generateApaTests() []createInstanceTest {
	var tests []createInstanceTest

	r := defaultApaInstanceParam
	t := createInstanceTest{
		name:     "valid sample apa",
		template: sample_apa.TemplateName,
		attrs:    defaultApaAttributes,
		param:    &r,
		expect:   defaultApaInstance,
	}
	tests = append(tests, t)

	return tests
}

var defaultQuotaAttributes = map[string]interface{}{
	"ats": time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
	"adr": 10 * time.Second,
	"as":  "val as",
	"as1": "val as1",
	"as2": "val as2",
	"as3": "val as3",
	"as4": "val as4",
	"as5": "val as5",
	"as6": "val as6",
	"ai":  int64(100),
	"ai2": int64(200),
	"ai3": int64(300),
	"ai4": int64(400),
	"ai5": int64(500),
	"ai6": int64(600),
	"ai7": int64(700),
	"ad":  float64(42.42),
	"ab":  true,
}

// a default Quota Instance Parameter. All of the fields can be calculated, given the values in the
// default bag.
var defaultQuotaInstanceParam = sample_quota.InstanceParam{
	BoolMap:    map[string]string{"ab": "ab"},
	Dimensions: map[string]string{"as": "as"},
	Res1: &sample_quota.Res1InstanceParam{
		Int64Primitive:  "ai",
		BoolPrimitive:   "ab",
		DoublePrimitive: "ad",
		TimeStamp:       "ats",
		Duration:        "adr",
		StringPrimitive: "as2",
		Int64Map:        map[string]string{"i2": "ai2"},
		Value:           "as3",
		Dimensions:      map[string]string{"i3": "ai3"},
		Res2: &sample_quota.Res2InstanceParam{
			Value:          "as4",
			Int64Primitive: "ai4",
			Dimensions:     map[string]string{"i5": "ai5"},
		},
		Res2Map: map[string]*sample_quota.Res2InstanceParam{
			"r2": {
				Int64Primitive: "ai6",
				Value:          "as5",
				Dimensions:     map[string]string{"i5": "ai5"},
			},
		},
	},
}

// given the default instance param and the default attributes, this is the expected quota instance. The name
// "instance1" is hard-coded.
var defaultQuotaInstance = &sample_quota.Instance{
	Name:       "instance1",
	BoolMap:    map[string]bool{"ab": true},
	Dimensions: map[string]interface{}{"as": "val as"},
	Res1: &sample_quota.Res1{
		Int64Primitive:  int64(100),
		BoolPrimitive:   true,
		DoublePrimitive: float64(42.42),
		TimeStamp:       time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
		Duration:        10 * time.Second,
		StringPrimitive: "val as2",
		Int64Map:        map[string]int64{"i2": int64(200)},
		Value:           "val as3",
		Dimensions:      map[string]interface{}{"i3": int64(300)},
		Res2: &sample_quota.Res2{
			Value:          "val as4",
			Int64Primitive: int64(400),
			Dimensions:     map[string]interface{}{"i5": int64(500)},
		},
		Res2Map: map[string]*sample_quota.Res2{
			"r2": {
				Int64Primitive: int64(600),
				Value:          "val as5",
				Dimensions:     map[string]interface{}{"i5": int64(500)},
			},
		},
	},
}

func generateQuotaTests() []createInstanceTest {
	var tests []createInstanceTest

	r := defaultQuotaInstanceParam
	t := createInstanceTest{
		name:     "valid sample quota",
		template: sample_quota.TemplateName,
		attrs:    defaultQuotaAttributes,
		param:    &r,
		expect:   defaultQuotaInstance,
	}
	tests = append(tests, t)

	return tests
}

var defaultCheckAttributes = map[string]interface{}{
	"ats": time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
	"adr": 10 * time.Second,
	"as":  "val as",
	"as1": "val as1",
	"as2": "val as2",
	"as3": "val as3",
	"as4": "val as4",
	"as5": "val as5",
	"as6": "val as6",
	"ai":  int64(100),
	"ai2": int64(200),
	"ai3": int64(300),
	"ai4": int64(400),
	"ai5": int64(500),
	"ai6": int64(600),
	"ai7": int64(700),
	"ad":  float64(42.42),
	"ab":  true,
}

// a default Check Instance Parameter. All of the fields can be calculated, given the values in the
// default bag.
var defaultCheckInstanceParam = sample_check.InstanceParam{
	CheckExpression: "as",
	StringMap:       map[string]string{"a": "as2"},
	Res1: &sample_check.Res1InstanceParam{
		StringPrimitive: "as3",
		Duration:        "adr",
		TimeStamp:       "ats",
		DoublePrimitive: "ad",
		BoolPrimitive:   "ab",
		Int64Primitive:  "ai",
		Value:           "as4",
		Dimensions:      map[string]string{"d1": "ai2"},
		Int64Map:        map[string]string{"i1": "ai3"},
		Res2: &sample_check.Res2InstanceParam{
			Value:          "as5",
			Int64Primitive: "ai4",
			Dimensions:     map[string]string{"d2": "ai5"},
		},
		Res2Map: map[string]*sample_check.Res2InstanceParam{
			"r2": {
				Value:          "as6",
				Int64Primitive: "ai6",
				Dimensions:     map[string]string{"d3": "ai7"},
			},
		},
	},
}

// given the default instance param and the default attributes, this is the expected check instance. The name
// "instance1" is hard-coded.
var defaultCheckInstance = &sample_check.Instance{
	Name:            "instance1",
	CheckExpression: "val as",
	StringMap:       map[string]string{"a": "val as2"},
	Res1: &sample_check.Res1{
		StringPrimitive: "val as3",
		Duration:        10 * time.Second,
		TimeStamp:       time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
		DoublePrimitive: float64(42.42),
		BoolPrimitive:   true,
		Int64Primitive:  int64(100),
		Value:           "val as4",
		Dimensions:      map[string]interface{}{"d1": int64(200)},
		Int64Map:        map[string]int64{"i1": int64(300)},
		Res2: &sample_check.Res2{
			Value:          "val as5",
			Int64Primitive: int64(400),
			Dimensions:     map[string]interface{}{"d2": int64(500)},
		},
		Res2Map: map[string]*sample_check.Res2{
			"r2": {
				Value:          "val as6",
				Int64Primitive: int64(600),
				Dimensions:     map[string]interface{}{"d3": int64(700)},
			},
		},
	},
}

func generateCheckTests() []createInstanceTest {
	var tests []createInstanceTest

	r := defaultCheckInstanceParam
	t := createInstanceTest{
		name:     "valid sample check",
		template: sample_check.TemplateName,
		attrs:    defaultCheckAttributes,
		param:    &r,
		expect:   defaultCheckInstance,
	}
	tests = append(tests, t)

	r2 := defaultCheckInstanceParam
	r2.CheckExpression = "badAttr"
	t = createInstanceTest{
		name:              "Check invalid attr1",
		template:          sample_check.TemplateName,
		attrs:             defaultCheckAttributes,
		param:             &r2,
		expectCreateError: "expression compilation failed at [instance1]'CheckExpression'",
	}
	tests = append(tests, t)

	r3 := defaultCheckInstanceParam
	r3.StringMap = map[string]string{"foo": "badAttr"}
	t = createInstanceTest{
		name:              "Check invalid attr2",
		template:          sample_check.TemplateName,
		attrs:             defaultCheckAttributes,
		param:             &r3,
		expectCreateError: "expression compilation failed at [instance1]'StringMap[foo]",
	}
	tests = append(tests, t)

	r4 := defaultCheckInstanceParam
	res1 := *r4.Res1
	res1.Int64Primitive = "badAttr"
	r4.Res1 = &res1
	t = createInstanceTest{
		name:              "Check invalid attr3",
		template:          sample_check.TemplateName,
		attrs:             defaultCheckAttributes,
		param:             &r4,
		expectCreateError: "expression compilation failed at [instance1]'Res1.Int64Primitive",
	}
	tests = append(tests, t)

	r5 := defaultCheckInstanceParam
	res2 := *r5.Res1
	res3 := *res1.Res2
	res3.Int64Primitive = "badAttr"
	res2.Res2 = &res3
	r5.Res1 = &res2
	t = createInstanceTest{
		name:              "Check invalid attr3",
		template:          sample_check.TemplateName,
		attrs:             defaultCheckAttributes,
		param:             &r5,
		expectCreateError: "expression compilation failed at [instance1]'Res1.Res2.Int64Primitive",
	}
	tests = append(tests, t)

	return tests
}

var defaultReportAttributes = map[string]interface{}{
	"ats": time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
	"adr": 10 * time.Second,
	"ap1": []byte(net.ParseIP("2.3.4.5")),
}

// a default Report Instance Parameter. All of the fields can be calculated, given the values in the
// default bag.
var defaultReportInstanceParam = sample_report.InstanceParam{
	Value:           "1",
	Dimensions:      map[string]string{"s": "2", "p": "ap1"},
	BoolPrimitive:   "true",
	DoublePrimitive: "1.2",
	Int64Primitive:  "54362",
	StringPrimitive: `"mystring"`,
	Int64Map:        map[string]string{"a": "1"},
	TimeStamp:       "ats",
	Duration:        "adr",
	Res1: &sample_report.Res1InstanceParam{
		Value:           "1",
		Dimensions:      map[string]string{"s": "2"},
		BoolPrimitive:   "true",
		DoublePrimitive: "1.2",
		Int64Primitive:  "54362",
		StringPrimitive: `"mystring"`,
		Int64Map:        map[string]string{"a": "1"},
		TimeStamp:       "ats",
		Duration:        "adr",
		Res2: &sample_report.Res2InstanceParam{
			Value:          "1",
			Dimensions:     map[string]string{"s": "2"},
			Int64Primitive: "54362",
			DnsName:        `"myDNS"`,
			Duration:       "adr",
			EmailAddr:      `"myEMAIL"`,
			IpAddr:         `ip("0.0.0.0")`,
			TimeStamp:      "ats",
			Uri:            `"myURI"`,
		},
		Res2Map: map[string]*sample_report.Res2InstanceParam{
			"foo": {
				Value:          "1",
				Dimensions:     map[string]string{"s": "2"},
				Int64Primitive: "54362",
				DnsName:        `"myDNS"`,
				Duration:       "adr",
				EmailAddr:      `"myEMAIL"`,
				IpAddr:         `ip("0.0.0.0")`,
				TimeStamp:      "ats",
				Uri:            `"myURI"`,
			},
		},
	},
}

// given the default instance param and the default attributes, this is the expected instance. The name
// "instance1" is hard-coded.
var defaultReportInstance = &sample_report.Instance{
	Name:            "instance1",
	Value:           int64(1),
	Dimensions:      map[string]interface{}{"s": int64(2), "p": []byte(net.ParseIP("2.3.4.5"))},
	BoolPrimitive:   true,
	DoublePrimitive: 1.2,
	Int64Primitive:  54362,
	StringPrimitive: "mystring",
	Int64Map:        map[string]int64{"a": int64(1)},
	TimeStamp:       time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
	Duration:        10 * time.Second,
	Res1: &sample_report.Res1{
		Value:           int64(1),
		Dimensions:      map[string]interface{}{"s": int64(2)},
		BoolPrimitive:   true,
		DoublePrimitive: 1.2,
		Int64Primitive:  54362,
		StringPrimitive: "mystring",
		Int64Map:        map[string]int64{"a": int64(1)},
		TimeStamp:       time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
		Duration:        10 * time.Second,
		Res2: &sample_report.Res2{
			Value:          int64(1),
			Dimensions:     map[string]interface{}{"s": int64(2)},
			Int64Primitive: 54362,
			DnsName:        adapter.DNSName("myDNS"),
			Duration:       10 * time.Second,
			EmailAddr:      adapter.EmailAddress("myEMAIL"),
			IpAddr:         net.ParseIP("0.0.0.0"),
			TimeStamp:      time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
			Uri:            adapter.URI("myURI"),
		},
		Res2Map: map[string]*sample_report.Res2{
			"foo": {
				Value:          int64(1),
				Dimensions:     map[string]interface{}{"s": int64(2)},
				Int64Primitive: 54362,
				DnsName:        adapter.DNSName("myDNS"),
				Duration:       10 * time.Second,
				EmailAddr:      adapter.EmailAddress("myEMAIL"),
				IpAddr:         net.ParseIP("0.0.0.0"),
				TimeStamp:      time.Date(2017, time.January, 1, 0, 0, 0, 0, time.UTC),
				Uri:            adapter.URI("myURI"),
			},
		},
	},
}

func generateReportTests() []createInstanceTest {
	var tests []createInstanceTest

	r := defaultReportInstanceParam // make a copyAttrs
	t := createInstanceTest{
		name:     "valid sample report",
		template: sample_report.TemplateName,
		attrs:    defaultReportAttributes,
		param:    &r,
		expect:   defaultReportInstance,
	}
	tests = append(tests, t)

	t = createInstanceTest{
		name:     "nil param returns nil instance",
		template: sample_report.TemplateName,
		attrs:    defaultReportAttributes,
		param:    nil,
		expect:   nil,
	}
	tests = append(tests, t)

	emptyFieldsParam := sample_report.InstanceParam{
		Res1: &sample_report.Res1InstanceParam{ // missing all fields
		},
	}
	t = createInstanceTest{
		name:     "unspecified fields in param - valid",
		template: sample_report.TemplateName,
		attrs:    defaultReportAttributes,
		param:    &emptyFieldsParam,
		expect: &sample_report.Instance{
			Name:            "instance1",
			Value:           nil,
			Dimensions:      map[string]interface{}{},
			BoolPrimitive:   false,
			DoublePrimitive: 0.0,
			Int64Primitive:  0,
			StringPrimitive: "",
			Int64Map:        map[string]int64{},
			TimeStamp:       time.Time{},
			Duration:        time.Duration(0),
			Res1: &sample_report.Res1{
				Value:           nil,
				Dimensions:      map[string]interface{}{},
				BoolPrimitive:   false,
				DoublePrimitive: 0.0,
				Int64Primitive:  0,
				StringPrimitive: "",
				Int64Map:        map[string]int64{},
				TimeStamp:       time.Time{},
				Duration:        time.Duration(0),
				Res2:            nil,
				Res2Map:         map[string]*sample_report.Res2{},
			},
		},
	}
	tests = append(tests, t)

	badParam1 := r
	badParam1.Value = "some-nonsense-expression%here"
	t = createInstanceTest{
		name:              "basic compilation error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam1,
		expectCreateError: "expression compilation failed at [instance1]'Value'",
	}
	tests = append(tests, t)

	badParam2 := r
	badParam2.Value = "ai"
	t = createInstanceTest{
		name:             "basic build error",
		template:         sample_report.TemplateName,
		attrs:            defaultReportAttributes,
		param:            &badParam2,
		expectBuildError: "evaluation failed at [instance1]'Value': 'lookup failed: 'ai'",
	}
	tests = append(tests, t)

	badParam3 := r
	badParam3.Dimensions = map[string]string{"foo": "ai"}
	t = createInstanceTest{
		name:             "Sample Report Dimensions Build Error",
		template:         sample_report.TemplateName,
		attrs:            defaultReportAttributes,
		param:            &badParam3,
		expectBuildError: "evaluation failed at [instance1]'Dimensions[foo]'",
	}
	tests = append(tests, t)

	badParam4 := r
	badParam4.Dimensions = map[string]string{"foo": "badAttr"}
	t = createInstanceTest{
		name:              "Sample Report Dimensions Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam4,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badParam5 := r
	badParam5.Int64Primitive = "badAttr"
	t = createInstanceTest{
		name:              "Sample Report Int64Primitive Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam5,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badParam12 := r
	badParam12.Int64Primitive = "as"
	t = createInstanceTest{
		name:              "Sample Report Int64Primitive Type Mismatch Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam12,
		expectCreateError: "expression compilation failed at [instance1]'Int64Primitive': 'instance field type mismatch: expected='INT64', actual='STRING'",
	}
	tests = append(tests, t)

	badParam18 := r
	badParam18.Int64Primitive = "ai"
	t = createInstanceTest{
		name:             "Sample Report Int64Primitive Build Error",
		template:         sample_report.TemplateName,
		attrs:            defaultReportAttributes,
		param:            &badParam18,
		expectBuildError: "evaluation failed at [instance1]'Int64Primitive':",
	}
	tests = append(tests, t)

	badParam6 := r
	badParam6.BoolPrimitive = "badAttr"
	t = createInstanceTest{
		name:              "Sample Report BoolPrimitive Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam6,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badParam13 := r
	badParam13.BoolPrimitive = "ai"
	t = createInstanceTest{
		name:              "Sample Report BoolPrimitive Type Mismatch Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam13,
		expectCreateError: "expression compilation failed at [instance1]'BoolPrimitive': 'instance field type mismatch: expected='BOOL', actual='INT64',",
	}
	tests = append(tests, t)

	badParam19 := r
	badParam19.BoolPrimitive = "ab"
	t = createInstanceTest{
		name:             "Sample Report BoolPrimitive Build Error",
		template:         sample_report.TemplateName,
		attrs:            defaultReportAttributes,
		param:            &badParam19,
		expectBuildError: "evaluation failed at [instance1]'BoolPrimitive':",
	}
	tests = append(tests, t)

	badParam7 := r
	badParam7.DoublePrimitive = "badAttr"
	t = createInstanceTest{
		name:              "Sample Report DoublePrimitive Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam7,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badParam14 := r
	badParam14.DoublePrimitive = "ai"
	t = createInstanceTest{
		name:              "Sample Report DoublePrimitive Type Mismatch Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam14,
		expectCreateError: "expression compilation failed at [instance1]'DoublePrimitive': 'instance field type mismatch: expected='DOUBLE', actual='INT64'",
	}
	tests = append(tests, t)

	badParam20 := r
	badParam20.DoublePrimitive = "ad"
	t = createInstanceTest{
		name:             "Sample Report DoublePrimitive Build Error",
		template:         sample_report.TemplateName,
		attrs:            defaultReportAttributes,
		param:            &badParam20,
		expectBuildError: "evaluation failed at [instance1]'DoublePrimitive':",
	}
	tests = append(tests, t)

	badParam8 := r
	badParam8.StringPrimitive = "badAttr"
	t = createInstanceTest{
		name:              "Sample Report StringPrimitive Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam8,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badParam15 := r
	badParam15.StringPrimitive = "ai"
	t = createInstanceTest{
		name:              "Sample Report StringPrimitive Type Mismatch Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam15,
		expectCreateError: "expression compilation failed at [instance1]'StringPrimitive': 'instance field type mismatch: expected='STRING', actual='INT64'",
	}
	tests = append(tests, t)

	badParam21 := r
	badParam21.StringPrimitive = "as"
	t = createInstanceTest{
		name:             "Sample Report StringPrimitive Build Error",
		template:         sample_report.TemplateName,
		attrs:            defaultReportAttributes,
		param:            &badParam21,
		expectBuildError: "evaluation failed at [instance1]'StringPrimitive':",
	}
	tests = append(tests, t)

	badParam9 := r
	badParam9.Int64Map = map[string]string{"foo": "badAttr"}
	t = createInstanceTest{
		name:              "Sample Report Int64Map Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam9,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badParam16 := r
	badParam16.Int64Map = map[string]string{"foo": "as"}
	t = createInstanceTest{
		name:              "Sample Report Int64Map Type Mismatch Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam16,
		expectCreateError: "expression compilation failed at [instance1]'Int64Map[foo]': 'instance field type mismatch: expected='INT64', actual='STRING',",
	}
	tests = append(tests, t)

	badParam22 := r
	badParam22.Int64Map = map[string]string{"foo": "ai"}
	t = createInstanceTest{
		name:             "Sample Report Int64Map Build Error",
		template:         sample_report.TemplateName,
		attrs:            defaultReportAttributes,
		param:            &badParam22,
		expectBuildError: "evaluation failed at [instance1]'Int64Map[foo]'",
	}
	tests = append(tests, t)

	badParam10 := r
	badParam10.TimeStamp = "badAttr"
	t = createInstanceTest{
		name:              "Sample Report Timestamp Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam10,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badAttrs1 := copyAttrs(defaultReportAttributes)
	delete(badAttrs1, "ats")
	t = createInstanceTest{
		name:             "Sample Report Timestamp Evaluation Error",
		template:         sample_report.TemplateName,
		attrs:            badAttrs1,
		param:            &defaultReportInstanceParam,
		expectBuildError: "evaluation failed at [instance1]'TimeStamp'",
	}
	tests = append(tests, t)

	badParam11 := r
	badParam11.Duration = "badAttr"
	t = createInstanceTest{
		name:              "Sample Report Duration Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam11,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badAttrs2 := copyAttrs(defaultReportAttributes)
	delete(badAttrs2, "adr")
	t = createInstanceTest{
		name:             "Sample Report Duration Evaluation Error",
		template:         sample_report.TemplateName,
		attrs:            badAttrs2,
		param:            &defaultReportInstanceParam,
		expectBuildError: "evaluation failed at [instance1]'Duration'",
	}
	tests = append(tests, t)

	badParam17 := r
	badRes1 := *r.Res1
	badRes1.Duration = "badAttr"
	badParam17.Res1 = &badRes1
	t = createInstanceTest{
		name:              "Sample Report Res1.Duration Compile Error",
		template:          sample_report.TemplateName,
		attrs:             defaultReportAttributes,
		param:             &badParam17,
		expectCreateError: "unknown attribute badAttr",
	}
	tests = append(tests, t)

	badParam23 := r
	badRes2 := *r.Res1
	badRes2.Duration = "ats2"
	badParam23.Res1 = &badRes2
	t = createInstanceTest{
		name:             "Sample Report Res1.Duration Evaluation Error",
		template:         sample_report.TemplateName,
		attrs:            defaultReportAttributes,
		param:            &badParam23,
		expectBuildError: "evaluation failed at [instance1]'Res1.Duration'",
	}
	tests = append(tests, t)

	return tests
}

func assertErr(t *testing.T, context string, expected string, actual error) {
	t.Helper()
	if expected == "" {
		if actual != nil {
			t.Fatalf("[%s] got error = %s, want success", context, actual.Error())
		}
	} else {
		if actual == nil || !strings.Contains(actual.Error(), expected) {
			t.Fatalf("[%s] get error = '%v', want '%s'", context, actual, expected)
		}
	}
}
