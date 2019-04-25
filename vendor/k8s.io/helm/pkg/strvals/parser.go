/*
Copyright The Helm Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package strvals

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
)

// ErrNotList indicates that a non-list was treated as a list.
var ErrNotList = errors.New("not a list")

// ToYAML takes a string of arguments and converts to a YAML document.
func ToYAML(s string) (string, error) {
	m, err := Parse(s)
	if err != nil {
		return "", err
	}
	d, err := yaml.Marshal(m)
	return string(d), err
}

// Parse parses a set line.
//
// A set line is of the form name1=value1,name2=value2
func Parse(s string) (map[string]interface{}, error) {
	vals := map[string]interface{}{}
	scanner := bytes.NewBufferString(s)
	t := newParser(scanner, vals, false)
	err := t.parse()
	return vals, err
}

// ParseFile parses a set line, but its final value is loaded from the file at the path specified by the original value.
//
// A set line is of the form name1=path1,name2=path2
//
// When the files at path1 and path2 contained "val1" and "val2" respectively, the set line is consumed as
// name1=val1,name2=val2
func ParseFile(s string, runesToVal runesToVal) (map[string]interface{}, error) {
	vals := map[string]interface{}{}
	scanner := bytes.NewBufferString(s)
	t := newFileParser(scanner, vals, runesToVal)
	err := t.parse()
	return vals, err
}

// ParseString parses a set line and forces a string value.
//
// A set line is of the form name1=value1,name2=value2
func ParseString(s string) (map[string]interface{}, error) {
	vals := map[string]interface{}{}
	scanner := bytes.NewBufferString(s)
	t := newParser(scanner, vals, true)
	err := t.parse()
	return vals, err
}

// ParseInto parses a strvals line and merges the result into dest.
//
// If the strval string has a key that exists in dest, it overwrites the
// dest version.
func ParseInto(s string, dest map[string]interface{}) error {
	scanner := bytes.NewBufferString(s)
	t := newParser(scanner, dest, false)
	return t.parse()
}

// ParseIntoFile parses a filevals line and merges the result into dest.
//
// This method always returns a string as the value.
func ParseIntoFile(s string, dest map[string]interface{}, runesToVal runesToVal) error {
	scanner := bytes.NewBufferString(s)
	t := newFileParser(scanner, dest, runesToVal)
	return t.parse()
}

// ParseIntoString parses a strvals line and merges the result into dest.
//
// This method always returns a string as the value.
func ParseIntoString(s string, dest map[string]interface{}) error {
	scanner := bytes.NewBufferString(s)
	t := newParser(scanner, dest, true)
	return t.parse()
}

// parser is a simple parser that takes a strvals line and parses it into a
// map representation.
//
// where sc is the source of the original data being parsed
// where data is the final parsed data from the parses with correct types
// where st is a boolean to figure out if we're forcing it to parse values as string
type parser struct {
	sc         *bytes.Buffer
	data       map[string]interface{}
	runesToVal runesToVal
}

type runesToVal func([]rune) (interface{}, error)

func newParser(sc *bytes.Buffer, data map[string]interface{}, stringBool bool) *parser {
	rs2v := func(rs []rune) (interface{}, error) {
		return typedVal(rs, stringBool), nil
	}
	return &parser{sc: sc, data: data, runesToVal: rs2v}
}

func newFileParser(sc *bytes.Buffer, data map[string]interface{}, runesToVal runesToVal) *parser {
	return &parser{sc: sc, data: data, runesToVal: runesToVal}
}

func (t *parser) parse() error {
	for {
		err := t.key(t.data)
		if err == nil {
			continue
		}
		if err == io.EOF {
			return nil
		}
		return err
	}
}

func runeSet(r []rune) map[rune]bool {
	s := make(map[rune]bool, len(r))
	for _, rr := range r {
		s[rr] = true
	}
	return s
}

func (t *parser) key(data map[string]interface{}) error {
	stop := runeSet([]rune{'=', '[', ',', '.'})
	for {
		switch k, last, err := runesUntil(t.sc, stop); {
		case err != nil:
			if len(k) == 0 {
				return err
			}
			return fmt.Errorf("key %q has no value", string(k))
			//set(data, string(k), "")
			//return err
		case last == '[':
			// We are in a list index context, so we need to set an index.
			i, err := t.keyIndex()
			if err != nil {
				return fmt.Errorf("error parsing index: %s", err)
			}
			kk := string(k)
			// Find or create target list
			list := []interface{}{}
			if _, ok := data[kk]; ok {
				list = data[kk].([]interface{})
			}

			// Now we need to get the value after the ].
			list, err = t.listItem(list, i)
			set(data, kk, list)
			return err
		case last == '=':
			//End of key. Consume =, Get value.
			// FIXME: Get value list first
			vl, e := t.valList()
			switch e {
			case nil:
				set(data, string(k), vl)
				return nil
			case io.EOF:
				set(data, string(k), "")
				return e
			case ErrNotList:
				rs, e := t.val()
				if e != nil && e != io.EOF {
					return e
				}
				v, e := t.runesToVal(rs)
				set(data, string(k), v)
				return e
			default:
				return e
			}

		case last == ',':
			// No value given. Set the value to empty string. Return error.
			set(data, string(k), "")
			return fmt.Errorf("key %q has no value (cannot end with ,)", string(k))
		case last == '.':
			// First, create or find the target map.
			inner := map[string]interface{}{}
			if _, ok := data[string(k)]; ok {
				inner = data[string(k)].(map[string]interface{})
			}

			// Recurse
			e := t.key(inner)
			if len(inner) == 0 {
				return fmt.Errorf("key map %q has no value", string(k))
			}
			set(data, string(k), inner)
			return e
		}
	}
}

func set(data map[string]interface{}, key string, val interface{}) {
	// If key is empty, don't set it.
	if len(key) == 0 {
		return
	}
	data[key] = val
}

func setIndex(list []interface{}, index int, val interface{}) []interface{} {
	if len(list) <= index {
		newlist := make([]interface{}, index+1)
		copy(newlist, list)
		list = newlist
	}
	list[index] = val
	return list
}

func (t *parser) keyIndex() (int, error) {
	// First, get the key.
	stop := runeSet([]rune{']'})
	v, _, err := runesUntil(t.sc, stop)
	if err != nil {
		return 0, err
	}
	// v should be the index
	return strconv.Atoi(string(v))

}
func (t *parser) listItem(list []interface{}, i int) ([]interface{}, error) {
	stop := runeSet([]rune{'[', '.', '='})
	switch k, last, err := runesUntil(t.sc, stop); {
	case len(k) > 0:
		return list, fmt.Errorf("unexpected data at end of array index: %q", k)
	case err != nil:
		return list, err
	case last == '=':
		vl, e := t.valList()
		switch e {
		case nil:
			return setIndex(list, i, vl), nil
		case io.EOF:
			return setIndex(list, i, ""), err
		case ErrNotList:
			rs, e := t.val()
			if e != nil && e != io.EOF {
				return list, e
			}
			v, e := t.runesToVal(rs)
			return setIndex(list, i, v), e
		default:
			return list, e
		}
	case last == '[':
		// now we have a nested list. Read the index and handle.
		i, err := t.keyIndex()
		if err != nil {
			return list, fmt.Errorf("error parsing index: %s", err)
		}
		// Now we need to get the value after the ].
		list2, err := t.listItem(list, i)
		return setIndex(list, i, list2), err
	case last == '.':
		// We have a nested object. Send to t.key
		inner := map[string]interface{}{}
		if len(list) > i {
			var ok bool
			inner, ok = list[i].(map[string]interface{})
			if !ok {
				// We have indices out of order. Initialize empty value.
				list[i] = map[string]interface{}{}
				inner = list[i].(map[string]interface{})
			}
		}

		// Recurse
		e := t.key(inner)
		return setIndex(list, i, inner), e
	default:
		return nil, fmt.Errorf("parse error: unexpected token %v", last)
	}
}

func (t *parser) val() ([]rune, error) {
	stop := runeSet([]rune{','})
	v, _, err := runesUntil(t.sc, stop)
	return v, err
}

func (t *parser) valList() ([]interface{}, error) {
	r, _, e := t.sc.ReadRune()
	if e != nil {
		return []interface{}{}, e
	}

	if r != '{' {
		t.sc.UnreadRune()
		return []interface{}{}, ErrNotList
	}

	list := []interface{}{}
	stop := runeSet([]rune{',', '}'})
	for {
		switch rs, last, err := runesUntil(t.sc, stop); {
		case err != nil:
			if err == io.EOF {
				err = errors.New("list must terminate with '}'")
			}
			return list, err
		case last == '}':
			// If this is followed by ',', consume it.
			if r, _, e := t.sc.ReadRune(); e == nil && r != ',' {
				t.sc.UnreadRune()
			}
			v, e := t.runesToVal(rs)
			list = append(list, v)
			return list, e
		case last == ',':
			v, e := t.runesToVal(rs)
			if e != nil {
				return list, e
			}
			list = append(list, v)
		}
	}
}

func runesUntil(in io.RuneReader, stop map[rune]bool) ([]rune, rune, error) {
	v := []rune{}
	for {
		switch r, _, e := in.ReadRune(); {
		case e != nil:
			return v, r, e
		case inMap(r, stop):
			return v, r, nil
		case r == '\\':
			next, _, e := in.ReadRune()
			if e != nil {
				return v, next, e
			}
			v = append(v, next)
		default:
			v = append(v, r)
		}
	}
}

func inMap(k rune, m map[rune]bool) bool {
	_, ok := m[k]
	return ok
}

func typedVal(v []rune, st bool) interface{} {
	val := string(v)

	if st {
		return val
	}

	if strings.EqualFold(val, "true") {
		return true
	}

	if strings.EqualFold(val, "false") {
		return false
	}

	if strings.EqualFold(val, "null") {
		return nil
	}

	if strings.EqualFold(val, "0") {
		return int64(0)
	}

	// If this value does not start with zero, try parsing it to an int
	if len(val) != 0 && val[0] != '0' {
		if iv, err := strconv.ParseInt(val, 10, 64); err == nil {
			return iv
		}
	}

	return val
}
