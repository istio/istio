package yaml

import (
	"testing"
	"github.com/gogo/protobuf/proto"
	"fmt"
	"istio.io/istio/mixer/pkg/protobuf/yaml/testdata/all"
	yaml2 "gopkg.in/yaml.v2"
	"reflect"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/ghodss/yaml"
)

func TestDecode(t *testing.T) {

	for _, tst := range []struct {
		x int
		l int
	}{
		{259, 1},
		{ 259, 2},
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
		name := fmt.Sprintf("x=%v,l=%v",tst.x, tst.l)
		t.Run(name, func(tt *testing.T) {
			test_x(uint64(tst.x), tst.l, tt)
		})
	}
}


func test_x(x uint64,  l int, t *testing.T) {
	ba := make([]byte, 0, 8)
	//ba := EncodeLength(x, l)
	ba = EncodeLength2(ba, x, l)

	if len(ba) < l {
		t.Fatalf("Incorrect length. got %v, want %v", len(ba), l)
	}
	x1, n := proto.DecodeVarint(ba)
	if x1 != x {
		t.Fatalf("Incorrect decode. got %v, want %v", x1, x)
	}
	if n != len(ba) {
		t.Fatalf("Not all args were consumed. got %v, want %v, %v", n, len(ba), ba)
	}
}

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

func TestStatic1 (t *testing.T) {
    data := map[interface{}] interface{} {}
	t.Logf("err = %v", yaml2.Unmarshal([]byte(sff), data))

	ff := foo.Simple{}

	t.Logf("err = %v", yaml2.Unmarshal([]byte(sff), &ff))

	ba, _ := yaml2.Marshal(ff)

	t.Logf("\n%s", string(ba))

	fds, err := getFileDescSet("testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	res := NewResolver(fds)

	db := NewDynamicEncoderBuilder(".foo.Simple", res, data, nil, true)

	de, err := db.Build()

	if err != nil {
		t.Fatalf("unable to build: %v", err)
	}

	ba = make([]byte, 0, 30)
	ba, err = de.Encode(nil, ba)
	if err != nil {
		t.Fatalf("unable to encode: %v", err)
	}
	t.Logf("ba = %v", ba)

	ff2 := foo.Simple{}
	err = ff2.Unmarshal(ba)
	if err != nil {
		t.Fatalf("unable to decode: %v", err)
	}

	t.Logf("ff2 = %v", ff2)

	ba, err = ff.Marshal()

	t.Logf("ba = %v", ba)

	_ = de
}


func TestStatic2(t *testing.T){
	fds, err := getFileDescSet("testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	res := NewResolver(fds)
	testMsg(t, sff2, res)
}

func TestStatic3(t *testing.T){
	fds, err := getFileDescSet("testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	res := NewResolver(fds)

	fs := &foo.Simple{
		MapStrStr: map[string] string {
			"kkk1": "vvv1",
		},
		RMsg:[]*foo.Other{ {B: true}, {Str: "OtherStrStr"},
		},
	}
	var ba string
	enc := jsonpb.Marshaler{}
	ba, err = enc.MarshalToString(fs)
	testMsg(t, ba, res)
}

func testMsg(t *testing.T, input string, res Resolver) {
	data := map[interface{}] interface{} {}
	var err error

	if err = yaml2.Unmarshal([]byte(input), data); err != nil {
		t.Fatalf("unable to unmarshal: %v\n%s", err, input)
	}


	var ba []byte

	if ba, err = yaml.YAMLToJSON([]byte(input)); err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	ff1 := foo.Simple{}
	if err = jsonpb.UnmarshalString(string(ba), &ff1); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	t.Logf("ff1 = %v", ff1)

	db := NewDynamicEncoderBuilder(".foo.Simple", res, data, nil, false)
	de, err := db.Build()

	if err != nil {
		t.Fatalf("unable to build: %v", err)
	}

	ba = make([]byte, 0, 30)
	ba, err = de.Encode(nil, ba)
	if err != nil {
		t.Fatalf("unable to encode: %v", err)
	}
	t.Logf("ba = %v", ba)

	ff2 := foo.Simple{}
	err = ff2.Unmarshal(ba)
	if err != nil {
		t.Fatalf("unable to decode: %v", err)
	}

	if ! reflect.DeepEqual(ff2, ff1) {
		t.Fatalf("got: %v, want: %v", ff2, ff1)
	}

	t.Logf("ff2 = %v", ff2)

	ba, err = ff1.Marshal()
	t.Logf("ba = %v", ba)
}