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
	"testing"

	config "istio.io/api/mixer/v1/config/descriptor"
)

func TestNewIndex(t *testing.T) {
	fn := newIndex()

	check(t, "ReturnType", fn.ReturnType(), config.STRING)
	check(t, "ArgTypes", fn.ArgTypes(), []config.ValueType{config.STRING_MAP, config.STRING})
}

func check(t *testing.T, msg string, got interface{}, want interface{}) {
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s got %#v\nwant %#v", msg, got, want)
	}
}

func TestNewEQ(t *testing.T) {
	fn := newEQ().(*eqFunc)
	tbl := []struct {
		val   interface{}
		match interface{}
		equal bool
	}{
		{"abc", "abc", true},
		{"abc", 5, false},
		{5, 5, true},
		{"ns1.svc.local", "ns1.*", true},
		{"ns1.svc.local", "ns2.*", false},
		{"svc1.ns1.cluster", "*.ns1.cluster", true},
		{"svc1.ns1.cluster", "*.ns1.cluster1", false},
		{[]uint8(net.ParseIP("10.3.25.1")), []uint8(net.ParseIP("10.3.25.1")), true},
		{[]uint8(net.ParseIP("10.3.25.1")), []uint8(net.ParseIP("103.4.15.3")), false},
		{[]uint8(net.ParseIP("2001:0db8:85a3:0000:0000:8a2e:0370:7334")), []uint8(net.ParseIP("2001:0db8:85a3:0000")), false},
		{[]byte{'a', 'b', 'e'}, []byte{'a', 'b', 'e'}, true},
		{[]byte{'a', 'b', 'e'}, []byte{'a', 'b', 'e', 'z', 'z', 'z'}, false},
	}
	for idx, tst := range tbl {
		t.Run(fmt.Sprintf("[%d] %s", idx, tst.val), func(t *testing.T) {
			rv := fn.call(tst.val, tst.match)
			if rv != tst.equal {
				t.Errorf("[%d] %v ?= %v -- got %#v\nwant %#v", idx, tst.val, tst.match, rv, tst.equal)
			}
		})
	}

	check(t, "ReturnType", fn.ReturnType(), config.BOOL)
	check(t, "ArgTypes", fn.ArgTypes(), []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED})
}

func TestNewIP(t *testing.T) {
	fn := newIP()

	check(t, "ReturnType", fn.ReturnType(), config.IP_ADDRESS)
	check(t, "ArgTypes", fn.ArgTypes(), []config.ValueType{config.STRING})
}

func TestNewTIMESTAMP(t *testing.T) {
	fn := newTIMESTAMP()

	check(t, "ReturnType", fn.ReturnType(), config.TIMESTAMP)
	check(t, "ArgTypes", fn.ArgTypes(), []config.ValueType{config.STRING})
}

func TestNewMatch(t *testing.T) {
	fn := newMatch()

	check(t, "ReturnType", fn.ReturnType(), config.BOOL)
	check(t, "ArgTypes", fn.ArgTypes(), []config.ValueType{config.STRING, config.STRING})
}

func TestNewLAND(t *testing.T) {
	fn := newLAND()

	check(t, "ReturnType", fn.ReturnType(), config.BOOL)
	check(t, "ArgTypes", fn.ArgTypes(), []config.ValueType{config.BOOL, config.BOOL})
}

func TestNewOR(t *testing.T) {
	fn := newOR()

	check(t, "ReturnType", fn.ReturnType(), config.VALUE_TYPE_UNSPECIFIED)
	check(t, "ArgTypes", fn.ArgTypes(), []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED})
}

func TestNewLOR(t *testing.T) {
	fn := newLOR()

	check(t, "ReturnType", fn.ReturnType(), config.BOOL)
	check(t, "ArgTypes", fn.ArgTypes(), []config.ValueType{config.BOOL, config.BOOL})
}
