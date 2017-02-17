// Copyright 2017 Google Inc.
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
	"strings"
	"testing"
	"time"

	"istio.io/mixer/pkg/attribute"
)

func TestGoodEval(tt *testing.T) {
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
			"a ",
			map[string]interface{}{
				"a": int64(2),
			},
			int64(2), "",
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
			int64(200), "",
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
			`service.name == "*.ns1.cluster" && service.user == "admin"`,
			map[string]interface{}{
				"service.name": "svc1.ns1.cluster",
				"service.user": "admin",
			},
			true, "",
		},
		{
			`(x/y) == 30`,
			map[string]interface{}{
				"x": int64(20),
				"y": int64(10),
			},
			false, "unknown function: QUO",
		},
		// The following will not work until we support
		// map as an attribute type
		{
			`request.header["X-FORWARDED-HOST"] == "aaa"`,
			map[string]interface{}{
				"request.header": map[string]string{
					"X-FORWARDED-HOST": "bbb",
				},
				"y": int64(10),
			},
			true, "unresolved attribute",
		},
	}

	for idx, tst := range tests {
		tt.Run(fmt.Sprintf("[%d] %s", idx, tst.src), func(t *testing.T) {
			attrs := &bag{attrs: tst.tmap}
			exp, err := Parse(tst.src)
			if err != nil {
				t.Errorf("[%d] unexpected error: %s", idx, err)
				return
			}
			res, err := exp.Eval(attrs, FuncMap())
			if err != nil {
				if tst.err == "" {
					t.Errorf("[%d] unexpected error: %s", idx, err)
				} else if !strings.Contains(err.Error(), tst.err) {
					t.Errorf("[%d] got %s\nwant %s", idx, err, tst.err)
				}
				return
			}
			if res != tst.result {
				t.Errorf("[%d] got %s\nwant %s", idx, res, tst.result)
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
			true, "parse error", anyType,
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
	ev := NewCEXLEvaluator()
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

func TestCexlValidate(tt *testing.T) {
	success := "_SUCCESS_"
	tests := []struct {
		s   string
		err string
	}{
		{"a", success},
		{"a=b", "parse error"},
	}

	ev := NewCEXLEvaluator()

	for idx, tst := range tests {
		tt.Run(fmt.Sprintf("[%d] %s", idx, tst.s), func(t *testing.T) {
			err := ev.Validate(tst.s)
			if (err == nil) != (tst.err == success) {
				t.Errorf("[%d] got %s, want %s", idx, err, tst.err)
				return
			}
			// check if error is of the correct type
			if err != nil && !strings.Contains(err.Error(), tst.err) {
				t.Errorf("[%d] got %s, want %s", idx, err, tst.err)
			}
		})
	}
}

// fake bag
type bag struct {
	attribute.Bag
	attrs map[string]interface{}
}

func (b *bag) String(name string) (string, bool) {
	c, found := b.attrs[name]
	if !found {
		return "", false
	}
	s, found := c.(string)
	if !found {
		return "", false
	}
	return s, true
}

// Int64 returns the named attribute if it exists.
func (b *bag) Int64(name string) (int64, bool) {
	c, found := b.attrs[name]
	if !found {
		return 0, false
	}
	if _, found = c.(int64); !found {
		return 0, false
	}

	return c.(int64), true
}

// Float64 returns the named attribute if it exists.
func (b *bag) Float64(name string) (float64, bool) {
	c, found := b.attrs[name]
	if !found {
		return 0.0, false
	}
	if _, found = c.(float64); !found {
		return 0.0, false
	}

	return c.(float64), true
}

// Bool returns the named attribute if it exists.
func (b *bag) Bool(name string) (bool, bool) {
	c, found := b.attrs[name]
	if !found {
		return false, false
	}
	if _, found = c.(bool); !found {
		return false, false
	}

	return c.(bool), true
}

func (b *bag) StringMap(name string) (map[string]string, bool) {
	c, found := b.attrs[name]
	if !found {
		return nil, false
	}
	if _, found = c.(map[string]string); !found {
		return nil, false
	}

	return c.(map[string]string), true

}
func (b *bag) Time(name string) (tt time.Time, bb bool) { return }

// Duration returns the named attribute if it exists.
func (b *bag) Duration(name string) (tt time.Duration, bb bool) { return }

// Bytes returns the named attribute if it exists.
func (b *bag) Bytes(name string) (u []uint8, bb bool) { return }
