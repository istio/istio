package dynamic

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/onsi/gomega"
	"gopkg.in/d4l3k/messagediff.v1"
	yaml2 "gopkg.in/yaml.v2"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/compiled"
	protoyaml "istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/protobuf/yaml/testdata/all"
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

const eveything = `
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
dbl: 123.456
r_dbl:
- 123.123
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
  str: "mystring2"
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
    i64: 33333
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  - str: "mystring3"
    i64: 123
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  - str: "mystring3"
    i64: 123
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99123
      dbl: 99.99


#### map[string]string ####
map_str_str:
  key1: val1
  key2: val2

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

const eveything_in = `
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
dbl: 123.456
r_dbl:
- 123.123
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
  str: "'mystring2'"
  i64: 33333
  dbl: 333.333
  b: true
  inenum: "'INNERTHREE'"
  inmsg:
    str: "'myinnerstring'"
    i64: 99
    dbl: 99.99
r_oth:
  - str: "'mystring2'"
    i64: 33333
    dbl: 333.333
    b: true
    inenum: "'INNERTHREE'"
    inmsg:
      str: "'myinnerstring'"
      i64: 99
      dbl: 99.99
  - str: "'mystring3'"
    i64: 123
    dbl: 333.333
    b: true
    inenum: "'INNERTHREE'"
    inmsg:
      str: "'myinnerstring'"
      i64: 99
      dbl: 99.99
  - str: "'mystring3'"
    i64: 123
    dbl: 333.333
    b: true
    inenum: "'INNERTHREE'"
    inmsg:
      str: "'myinnerstring'"
      i64: 99123
      dbl: 99.99


#### map[string]string ####
map_str_str:
  key1: "'val1'"
  key2: "'val2'"

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
    key1: 123
map_str_sfixed64:
    key1: 123
map_str_sint32:
    key1: 123
map_str_sint64:
    key1: 123
`

type testdata struct {
	desc     string
	input    string
	output   string
	msg      string
	compiler compiled.Compiler
}

func TestDynamicEncoder(t *testing.T) {
	fds, err := protoyaml.GetFileDescSet("../testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	compiler := compiled.NewBuilder(statdardVocabulary())
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
			input:    eveything_in,
			output:   eveything,
			compiler: compiler,
		},
	} {
		t.Run(td.desc, func(tt *testing.T) {
			testMsg(tt, td.input, td.output, res, td.compiler, td.msg)
		})
	}
}

func TestStaticEncoder(t *testing.T) {
	fds, err := protoyaml.GetFileDescSet("../testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	//compiler := compiled.NewBuilder(statdardVocabulary())
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
			input: eveything,
		},
	} {
		t.Run(td.desc, func(tt *testing.T) {
			testMsg(tt, td.input, td.output, res, td.compiler, td.msg)
		})
	}
}

func testMsg(t *testing.T, input string, output string, res protoyaml.Resolver, compiler compiled.Compiler, msgName string) {
	g := gomega.NewGomegaWithT(t)

	data := map[interface{}]interface{}{}
	var err error

	if err = yaml2.Unmarshal([]byte(input), data); err != nil {
		t.Fatalf("unable to unmarshal: %v\n%s", err, input)
	}

	var ba []byte

	op := input
	if output != "" {
		op = output
	}

	if ba, err = yaml.YAMLToJSON([]byte(op)); err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	ff1 := foo.Simple{}
	if err = jsonpb.UnmarshalString(string(ba), &ff1); err != nil {
		t.Fatalf("failed to unmarshal: %v\n%v", err, string(ba))
	}

	t.Logf("ff1 = %v", ff1)
	ba, err = ff1.Marshal()
	if err != nil {
		t.Fatalf("unable to marshal origin message: %v", err)
	}
	t.Logf("ba1 = [%d] %v", len(ba), ba)

	db := NewEncoderBuilder(msgName, res, data, compiler, false)
	de, err := db.Build()

	if err != nil {
		t.Fatalf("unable to build: %v", err)
	}

	ba = make([]byte, 0, 30)
	bag := attribute.GetFakeMutableBagForTesting(map[string]interface{}{
		"request.reason":        "TWO",
		"response.size":         int64(200),
		"response.code":         int64(662),
		"source.service":        "a.svc.cluster.local",
		"connection.sent.bytes": int64(2),
		"test.double":           float64(3.1417),
		"test.bool":             true,
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

	// confirm that codegen'd code direct unmarshal and unmarhal thru bytes yields the same result.

	_ = g
	if !reflect.DeepEqual(ff2, ff1) {
		s, _ := messagediff.PrettyDiff(ff2, ff1)
		t.Logf("difference: %s", s)
		t.Fatalf("\n got: %v\nwant: %v", ff2, ff1)
	}

	t.Logf("ff2 = %v", ff2)

}

func statdardVocabulary() ast.AttributeDescriptorFinder {
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
	}

	return ast.NewFinder(attrs)
}
