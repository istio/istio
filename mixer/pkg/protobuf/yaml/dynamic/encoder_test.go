// Copyright Istio Authors.
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
package dynamic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	diff "gopkg.in/d4l3k/messagediff.v1"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/compiled"
	protoyaml "istio.io/istio/mixer/pkg/protobuf/yaml"
	foo "istio.io/istio/mixer/pkg/protobuf/yaml/testdata/all"
	"istio.io/istio/mixer/pkg/runtime/lang"
	"istio.io/pkg/attribute"
)

func TestEncodeVarintZeroExtend(t *testing.T) {
	for _, tst := range []struct {
		x int
		l int
	}{
		{259, 1},
		{259, 2},
		{259, 3},
		{259, 4},
		{7, 1},
		{7, 2},
		{7, 3},
		{10003432, 3},
		{10003432, 4},
		{10003432, 5},
		{10003432, 8},
	} {
		name := fmt.Sprintf("x=%v,l=%v", tst.x, tst.l)
		t.Run(name, func(tt *testing.T) {
			testEncodeVarintZeroExtend(uint64(tst.x), tst.l, tt)
		})
	}
}

func testEncodeVarintZeroExtend(x uint64, l int, tt *testing.T) {
	ba := make([]byte, 0, 8)
	ba = EncodeVarintZeroExtend(ba, x, l)

	if len(ba) < l {
		tt.Fatalf("Incorrect length. got %v, want %v", len(ba), l)
	}
	x1, n := proto.DecodeVarint(ba)
	if x1 != x {
		tt.Fatalf("Incorrect decode. got %v, want %v", x1, x)
	}
	if n != len(ba) {
		tt.Fatalf("Not all args were consumed. got %v, want %v, %v", n, len(ba), ba)
	}
}

const simple = `
str: "mystring"
i64: 56789
dbl: 123.456
b: true
enm: TWO
oth:
  str: "mystring2"
  i64: 33333
  dbl: 333.333
  b: true
  inenum: INNERTHREE
  inmsg:
    str: "myinnerstring"
    i64: 99
    dbl: 99.99
`
const sff2 = `
str: Str
dbl: 0.0021
i64: 10000203
b: true
oth:
    str: Oth.Str
mapStrStr:
    kk1: vv1
    kk2: vv2
mapI32Msg:
    200:
        str: Str
`
const sff = `
str: Str
dbl: 0.0021
i64: 10000203
b: true
`
const smm = `
mapStrStr:
    kk1: vv1
    kk2: vv2
`

const dmm = `
str: "'mystring'"
i64: response.size| 0
mapStrStr:
  source_service: source.service | "unknown"
  source_version: source.labels["version"] | "unknown"
oth:
  inenum: "'INNERTHREE'"
enm: request.reason
si32: -20 
si64: 200000002
r_enm:
  - 0
  - "'TWO'"
  - connection.sent.bytes
r_flt:
  - 1.12
  - 1.13
r_i64:
  - response.code
  - 770
`

const dmmOut = `
str: mystring
i64: 200
mapStrStr:
  source_service: a.svc.cluster.local
  source_version: v1
oth:
  inenum: INNERTHREE
enm: TWO
si32: -20
si64: 200000002
r_enm:
  - ONE
  - TWO
  - THREE
r_flt:
  - 1.12
  - 1.13
r_i64:
  - 662
  - 770
`

const everything = `
enm: TWO

r_enm:
- TWO
- THREE

#### string ####
str: "mystring"
r_str:
- abcd
- a.svc.cluster.local

#### bool ####
b: true
r_b:
- true
- false
- true

#### double ####
dbl: 3.1417
r_dbl:
- 3.1417
- 456.456

#### float ####
flt: 1.12
r_flt:
- 1.12
- 1.13

#### int64 with negative val ####
i64: 123
r_i64:
- 123
- -123

#### int32 with negative val ####
i32: 123
r_i32:
- -123

#### uint64 ####
ui64: 123
r_ui64:
- 123

#### uint32 ####
ui32: 123
r_ui32:
- 123

#### fixed64 ####
f64: 123
r_f64:
- 123

#### sfixed64 ####
sf64: 123
r_sf64:
- 123

#### fixed32 ####
f32: 123
r_f32:
- 123

#### sfixed32 ####
sf32: 123
r_sf32:
- 123

#### sint32 ####
si32: -123
r_si32:
- -789
- 123

#### sint64 ####
si64: -123
r_si64:
- -789
- 123

## sub-message ##
oth:
  str: "a.svc.cluster.local"
  i64: 33333
  dbl: 333.333
  b: true
  inenum: INNERTHREE
  inmsg:
    str: "myinnerstring"
    i64: 99
    dbl: 99.99
r_oth:
  - str: "mystring2"
    i64: 66666
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  - str: "mystring3"
    i64: 77777
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  - str: "mystring3"
    i64: 88888
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99123
      dbl: 99.99


#### map[string]string ####
map_str_str:
  key1: a.svc.cluster.local
  key2: INNERTHREE

#### map[string]message ####
map_str_msg:
  key1:
    str: "mystring2"
    i64: 33333
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  key2:
    str: "mystring2"
    i64: 33333
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99

#### map[int32]message ####
map_i32_msg:
  123:
    str: "mystring2"
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  456:
    str: "mystring2"

### map[int64]double ####
map_int64_double:
  123: 123.111
  456: 123.222

## other maps ##
map_str_float:
    key1: 123
map_str_uint64:
    key1: 123
map_str_uint32:
    key1: 123
map_str_fixed64:
    key1: 123
map_str_bool:
    key1: true
map_str_sfixed32:
    key1: 123
map_str_sfixed64:
    key1: 123
map_str_sint32:
    key1: 123
map_str_sint64:
    key1: 123
`

const everythingIn = `
enm: "'TWO'"

r_enm:
- request.reason
- "'THREE'"

#### string ####
str: "'mystring'"
r_str:
- "'abcd'"
- source.service

#### bool ####
b: test.bool
r_b:
- true
- false
- true

#### double ####
dbl: test.double
r_dbl:
- test.double
- 456.456

#### float ####
flt: test.float
r_flt:
- test.float
- 1.13

#### int64 with negative val ####
i64: test.i32
r_i64:
- 123
- test.i64

#### int32 with negative val ####
i32: test.i32
r_i32:
- -123

#### uint64 ####
ui64: test.i32
r_ui64:
- 123

#### uint32 ####
ui32: 123
r_ui32:
- test.i32

#### fixed64 ####
f64: 123
r_f64:
- test.i32

#### sfixed64 ####
sf64: test.i32
r_sf64:
- 123

#### fixed32 ####
f32: 123
r_f32:
- test.i32

#### sfixed32 ####
sf32: test.i32
r_sf32:
- 123

#### sint32 ####
si32: test.i64
r_si32:
- -789
- test.i32

#### sint64 ####
si64: test.i64
r_si64:
- -789
- test.i32

## sub-message ##
oth:
  str: source.service
  i64: 33333
  dbl: 333.333
  b: true
  inenum: request.path
  inmsg:
    str: "'myinnerstring'"
    i64: 99
    dbl: 99.99
r_oth:
  - str: "'mystring2'"
    i64: 66666
    dbl: 333.333
    b: true
    inenum: "'INNERTHREE'"
    inmsg:
      str: "'myinnerstring'"
      i64: 99
      dbl: 99.99
  - str: "'mystring3'"
    i64: 77777
    dbl: 333.333
    b: true
    inenum: request.path
    inmsg:
      str: "'myinnerstring'"
      i64: 99
      dbl: 99.99
  - str: "'mystring3'"
    i64: 88888
    dbl: 333.333
    b: true
    inenum: "'INNERTHREE'"
    inmsg:
      str: "'myinnerstring'"
      i64: 99123
      dbl: 99.99


#### map[string]string ####
map_str_str:
  key1: source.service
  key2: request.path

#### map[string]message ####
map_str_msg:
  key1:
    str: "'mystring2'"
    i64: 33333
    dbl: 333.333
    b: true
    inenum: "'INNERTHREE'"
    inmsg:
      str: "'myinnerstring'"
      i64: 99
      dbl: 99.99
  key2:
    str: "'mystring2'"
    i64: 33333
    dbl: 333.333
    b: true
    inenum: "'INNERTHREE'"
    inmsg:
      str: "'myinnerstring'"
      i64: 99
      dbl: 99.99

#### map[int32]message ####
map_i32_msg:
  123:
    str: "'mystring2'"
    inmsg:
      str: "'myinnerstring'"
      i64: 99
      dbl: 99.99
  456:
    str: "'mystring2'"

### map[int64]double ####
map_int64_double:
  123: 123.111
  456: 123.222

## other maps ##
map_str_float:
    key1: 123
map_str_uint64:
    key1: 123
map_str_uint32:
    key1: 123
map_str_fixed64:
    key1: 123
map_str_bool:
    key1: true
map_str_sfixed32:
    key1: test.i32
map_str_sfixed64:
    key1: 123
map_str_sint32:
    key1: test.i32
map_str_sint64:
    key1: 123
`

const valueStrIn = `
istio_value: test.i64
map_str_istio_value:
    test.i64: test.i64
    float: 5.5
    str: request.path
    ip: source.ip
ipaddress_istio_value: source.ip
map_str_ipaddress_istio_value:
    host1: source.ip
timestamp_istio_value: context.timestamp
duration_istio_value: response.duration
dnsname_istio_value: test.dns_name
uri_istio_value: test.uri
emailaddress_istio_value: test.email_address
`

const valueStr = `
istio_value:
    int64_value: -123
map_str_istio_value:
    test.i64: 
        int64_value: -123
    float:
         double_value: 5.5
    str:
         string_value: "INNERTHREE"
    ip:
         ip_address_value:
             value:
             - 8
             - 0
             - 0
             - 1

ipaddress_istio_value:
    value:
    - 8
    - 0
    - 0
    - 1
map_str_ipaddress_istio_value:
    host1:
        value:
        - 8
        - 0
        - 0
        - 1
timestamp_istio_value:
    value: 2018-08-15T00:00:01Z
duration_istio_value:
    value: 10s
dnsname_istio_value:
    value: google.com
uri_istio_value:
    value: https://maps.google.com
emailaddress_istio_value:
    value: istio@google.com
`

type testdata struct {
	desc        string
	input       string
	output      string
	msg         string
	compiler    lang.Compiler
	skipUnknown bool
}

func TestStaticPrecoded(t *testing.T) {
	fds, err := protoyaml.GetFileDescSet("../testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	compiler := compiled.NewBuilder(StandardVocabulary())
	res := protoyaml.NewResolver(fds)

	b := NewEncoderBuilder(res, compiler, false)

	oth := &foo.Other{
		Str: "foo.Other.Str",
	}
	golden := &foo.Simple{
		Str: "golden.str",
		Oth: oth,
	}

	var oEnc Encoder
	{
		if oEnc, err = b.BuildWithLength(".foo.other", map[string]interface{}{
			"str": `"foo.Other.Str"`,
		}); err != nil {
			t.Fatalf("Unable to get builder:%v", err)
		}

		var eBa []byte
		if eBa, err = oEnc.Encode(nil, eBa); err != nil {
			t.Fatalf("unable to encode: %v", oEnc)
		}

		// eBa is built with length, so read first bytes
		_, nBytes := proto.DecodeVarint(eBa)

		vOth := &foo.Other{}
		if err = vOth.Unmarshal(eBa[nBytes:]); err != nil {
			t.Fatalf("Unable to unmarshal: %v", err)
		}
		expectEqual(vOth, oth, t)
	}

	var enc Encoder
	{
		if enc, err = b.Build(".foo.Simple", map[string]interface{}{
			"str": `"golden.str"`,
			"oth": oEnc,
		}); err != nil {
			t.Fatalf("Unable to get builder:%v", err)
		}
		var eBa []byte
		if eBa, err = enc.Encode(nil, eBa); err != nil {
			t.Fatalf("unable to encode: %v", oEnc)
		}

		vOth := &foo.Simple{}
		if err = vOth.Unmarshal(eBa); err != nil {
			bBa, _ := golden.Marshal()
			t.Logf("\n got: %v\nwant: %v", eBa, bBa)
			oBa, _ := oth.Marshal()
			t.Logf("oth: %v", oBa)
			t.Fatalf("Unable to unmarshal: %v", err)
		}
		expectEqual(vOth, golden, t)
	}
}

func expectEqual(got interface{}, want interface{}, t *testing.T) {
	t.Helper()
	s, equal := diff.PrettyDiff(got, want)
	if equal {
		return
	}

	t.Logf("difference: %s", s)
	t.Fatalf("\n got: %v\nwant: %v", got, want)
}

func TestDynamicEncoder(t *testing.T) {
	fds, err := protoyaml.GetFileDescSet("../testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	compiler := compiled.NewBuilder(StandardVocabulary())
	res := protoyaml.NewResolver(fds)
	for _, td := range []testdata{
		{
			desc:     "metrics",
			msg:      ".foo.Simple",
			input:    dmm,
			output:   dmmOut,
			compiler: compiler,
		},
		{
			desc:     "metrics",
			msg:      ".foo.Simple",
			input:    everythingIn + valueStrIn,
			output:   everything + valueStr,
			compiler: compiler,
		},
	} {
		t.Run(td.desc, func(tt *testing.T) {
			testMsg(tt, td.input, td.output, res, td.compiler, td.msg, td.skipUnknown)
		})
	}
}

func TestStaticEncoder(t *testing.T) {
	fds, err := protoyaml.GetFileDescSet("../testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	res := protoyaml.NewResolver(fds)

	for _, td := range []testdata{
		{
			desc:  "map-only",
			msg:   ".foo.Simple",
			input: smm,
		},
		{
			desc:  "elementary",
			msg:   ".foo.Simple",
			input: sff,
		},
		{
			desc:  "full-message",
			msg:   ".foo.Simple",
			input: sff2,
		},
		{
			desc:  "full-message2",
			msg:   ".foo.Simple",
			input: simple,
		},
		{
			desc:  "everything",
			msg:   ".foo.Simple",
			input: everything,
		},
	} {
		fieldLengthSize = 0
		msgLengthSize = 0
		t.Run(td.desc+"-with-msg-copy", func(tt *testing.T) {
			testMsg(tt, td.input, td.output, res, td.compiler, td.msg, td.skipUnknown)
		})

		fieldLengthSize = defaultFieldLengthSize
		msgLengthSize = defaultMsgLengthSize
		t.Run(td.desc, func(tt *testing.T) {
			testMsg(tt, td.input, td.output, res, td.compiler, td.msg, td.skipUnknown)
		})
	}
}

func testMsg(t *testing.T, input string, output string, res protoyaml.Resolver,
	compiler lang.Compiler, msgName string, skipUnknown bool) {
	data := map[string]interface{}{}
	var err error
	var ba []byte

	if ba, err = yaml.YAMLToJSON([]byte(input)); err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if err = json.Unmarshal(ba, &data); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// build encoder
	db := NewEncoderBuilder(res, compiler, skipUnknown)
	de, err := db.Build(msgName, data)

	if err != nil {
		t.Fatalf("unable to build: %v", err)
	}

	// capture input via alternate process
	op := input
	if output != "" {
		op = output
	}

	if ba, err = yaml.YAMLToJSON([]byte(op)); err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	ff1 := foo.Simple{}
	um := jsonpb.Unmarshaler{AllowUnknownFields: skipUnknown}
	if err = um.Unmarshal(bytes.NewReader(ba), &ff1); err != nil {
		t.Fatalf("failed to unmarshal: %v\n%v", err, string(ba))
	}

	t.Logf("ff1 = %v", ff1)
	ba, err = ff1.Marshal()
	if err != nil {
		t.Fatalf("unable to marshal origin message: %v", err)
	}
	t.Logf("ba1 = [%d] %v", len(ba), ba)

	ba = make([]byte, 0, 30)
	bag := attribute.GetMutableBagForTesting(map[string]interface{}{
		"request.reason":        "TWO",
		"response.size":         int64(200),
		"response.code":         int64(662),
		"source.labels":         attribute.WrapStringMap(map[string]string{"version": "v1"}),
		"source.service":        "a.svc.cluster.local",
		"request.path":          "INNERTHREE",
		"connection.sent.bytes": int64(2),
		"test.double":           float64(3.1417),
		"test.float":            float64(1.12),
		"test.i64":              int64(-123),
		"test.i32":              int64(123),
		"test.bool":             true,
		"source.ip":             []byte{8, 0, 0, 1},
		"response.duration":     10 * time.Second,
		"context.timestamp":     time.Date(2018, 8, 15, 0, 0, 1, 0, time.UTC).UTC(),
		"test.dns_name":         "google.com",
		"test.uri":              "https://maps.google.com",
		"test.email_address":    "istio@google.com",
	})
	ba, err = de.Encode(bag, ba)
	if err != nil {
		t.Fatalf("unable to encode: %v", err)
	}
	t.Logf("ba2 = [%d] %v", len(ba), ba)

	ff2 := foo.Simple{}
	err = ff2.Unmarshal(ba)
	if err != nil {
		t.Fatalf("unable to decode: %v", err)
	}
	t.Logf("ff2 = %v", ff2)

	// confirm that codegen'd code direct unmarshal and unmarhal thru bytes yields the same result.

	if !reflect.DeepEqual(ff2, ff1) {
		s, _ := diff.PrettyDiff(ff2, ff1)
		t.Logf("difference: %s", s)
		t.Fatalf("\n got: %v\nwant: %v", ff2, ff1)
	} else {
		t.Logf("\n got: %v\nwant: %v", ff2, ff1)
	}
}

func Test_transFormQuotedString(t *testing.T) {
	for _, tst := range []struct {
		input  interface{}
		output interface{}
		found  bool
	}{
		{input: `'abc'`, output: "abc", found: true},
		{input: `.`, output: `.`, found: false},
		{input: `'`, output: `'`, found: false},
		{input: 23, output: 23, found: false},
	} {
		name := fmt.Sprintf("%v-%v", tst.input, tst.found)
		t.Run(name, func(t *testing.T) {
			op, ok := transformQuotedString(tst.input)

			if ok != tst.found {
				t.Fatalf("error in ok got:%v, want:%v", ok, tst.found)
			}

			if op != tst.output {
				t.Fatalf("error in output got:%v, want:%v", op, tst.output)
			}
		})
	}
}

// StandardVocabulary returns Istio standard vocabulary
func StandardVocabulary() attribute.AttributeDescriptorFinder {
	attrs := map[string]*v1beta1.AttributeManifest_AttributeInfo{
		"api.operation":                   {ValueType: v1beta1.STRING},
		"api.protocol":                    {ValueType: v1beta1.STRING},
		"api.service":                     {ValueType: v1beta1.STRING},
		"api.version":                     {ValueType: v1beta1.STRING},
		"connection.duration":             {ValueType: v1beta1.DURATION},
		"connection.id":                   {ValueType: v1beta1.STRING},
		"connection.received.bytes":       {ValueType: v1beta1.INT64},
		"connection.received.bytes_total": {ValueType: v1beta1.INT64},
		"connection.sent.bytes":           {ValueType: v1beta1.INT64},
		"connection.sent.bytes_total":     {ValueType: v1beta1.INT64},
		"context.protocol":                {ValueType: v1beta1.STRING},
		"context.time":                    {ValueType: v1beta1.TIMESTAMP},
		"context.timestamp":               {ValueType: v1beta1.TIMESTAMP},
		"destination.ip":                  {ValueType: v1beta1.IP_ADDRESS},
		"destination.labels":              {ValueType: v1beta1.STRING_MAP},
		"destination.name":                {ValueType: v1beta1.STRING},
		"destination.namespace":           {ValueType: v1beta1.STRING},
		"destination.service":             {ValueType: v1beta1.STRING},
		"destination.serviceAccount":      {ValueType: v1beta1.STRING},
		"destination.uid":                 {ValueType: v1beta1.STRING},
		"origin.ip":                       {ValueType: v1beta1.IP_ADDRESS},
		"origin.uid":                      {ValueType: v1beta1.STRING},
		"origin.user":                     {ValueType: v1beta1.STRING},
		"request.api_key":                 {ValueType: v1beta1.STRING},
		"request.auth.audiences":          {ValueType: v1beta1.STRING},
		"request.auth.presenter":          {ValueType: v1beta1.STRING},
		"request.auth.principal":          {ValueType: v1beta1.STRING},
		"request.auth.claims":             {ValueType: v1beta1.STRING_MAP},
		"request.headers":                 {ValueType: v1beta1.STRING_MAP},
		"request.host":                    {ValueType: v1beta1.STRING},
		"request.id":                      {ValueType: v1beta1.STRING},
		"request.method":                  {ValueType: v1beta1.STRING},
		"request.path":                    {ValueType: v1beta1.STRING},
		"request.reason":                  {ValueType: v1beta1.STRING},
		"request.referer":                 {ValueType: v1beta1.STRING},
		"request.scheme":                  {ValueType: v1beta1.STRING},
		"request.size":                    {ValueType: v1beta1.INT64},
		"request.time":                    {ValueType: v1beta1.TIMESTAMP},
		"request.useragent":               {ValueType: v1beta1.STRING},
		"response.code":                   {ValueType: v1beta1.INT64},
		"response.duration":               {ValueType: v1beta1.DURATION},
		"response.headers":                {ValueType: v1beta1.STRING_MAP},
		"response.size":                   {ValueType: v1beta1.INT64},
		"response.time":                   {ValueType: v1beta1.TIMESTAMP},
		"source.ip":                       {ValueType: v1beta1.IP_ADDRESS},
		"source.labels":                   {ValueType: v1beta1.STRING_MAP},
		"source.name":                     {ValueType: v1beta1.STRING},
		"source.namespace":                {ValueType: v1beta1.STRING},
		"source.service":                  {ValueType: v1beta1.STRING},
		"source.serviceAccount":           {ValueType: v1beta1.STRING},
		"source.uid":                      {ValueType: v1beta1.STRING},
		"source.user":                     {ValueType: v1beta1.STRING},
		"test.bool":                       {ValueType: v1beta1.BOOL},
		"test.double":                     {ValueType: v1beta1.DOUBLE},
		"test.i32":                        {ValueType: v1beta1.INT64},
		"test.i64":                        {ValueType: v1beta1.INT64},
		"test.float":                      {ValueType: v1beta1.DOUBLE},
		"test.uri":                        {ValueType: v1beta1.URI},
		"test.dns_name":                   {ValueType: v1beta1.DNS_NAME},
		"test.email_address":              {ValueType: v1beta1.EMAIL_ADDRESS},
	}

	return attribute.NewFinder(attrs)
}

func Test_Int64(t *testing.T) {
	for _, tst := range []struct {
		input  interface{}
		output interface{}
		found  bool
	}{
		{input: int(5), output: int64(5), found: true},
		{input: int64(5), output: int64(5), found: true},
		{input: 3.5, output: int64(3), found: false},
		{input: 3.0, output: int64(3), found: true},
	} {
		name := fmt.Sprintf("%v-%v", tst.input, tst.found)
		t.Run(name, func(t *testing.T) {
			op, ok := protoyaml.ToInt64(tst.input)
			if ok != tst.found {
				t.Fatalf("error in ok got:%v, want:%v", ok, tst.found)
			}

			if op != tst.output {
				t.Fatalf("error in output got:%v, want:%v", op, tst.output)
			}
		})
	}
}
