// Copyright Istio Authors
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

package adapter

import (
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"istio.io/pkg/pool"
)

// This file contains common utility functions that adapters need to process content
// of instances.

// Stringify converts basic data types, supported by `istio.mixer.v1.config.descriptor.ValueType`, into string.
// Note nil object is converted into empty "" string.
func Stringify(v interface{}) string {
	if v != nil {
		// Switch on all possible ValueTypes
		switch vv := v.(type) {
		case string:
			return vv
		case int64:
			return strconv.FormatInt(vv, 10)
		case float64:
			return strconv.FormatFloat(vv, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(vv)
		case []byte:
			return net.IP(vv).String()
		case time.Time:
			return string(serializeTime(vv))
		case time.Duration:
			return vv.String()
		case EmailAddress:
			return string(vv)
		case URI:
			return string(vv)
		case DNSName:
			return string(vv)
		case map[string]string:
			buffer := pool.GetBuffer()
			keys := make([]string, 0, len(vv))
			for k := range vv {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if buffer.Len() != 0 {
					buffer.WriteString("&")
				}
				buffer.WriteString(k)
				buffer.WriteString("=")
				buffer.WriteString(vv[k])
			}
			ret := buffer.String()
			pool.PutBuffer(buffer)
			return ret
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// StringEquals compares if string representations of two basic data types, supported by
// `istio.mixer.v1.config.descriptor.ValueType`, are equal.
// Note:
//   * string representation of nil object is an empty string, so StringEquals with
//     a nil object and a string of value empty "", will evaluate to true.
//   * This code is optimized for the case when one of the values is of string type.
//     For other cases, this code does reflect.DeepEquals which is not performant.
//   * For comparing a map to string representation of map, the string must be of the format
//     a=b&c=d, where a and c are keys in the map, and b and d are their corresponding values.
func StringEquals(a interface{}, b interface{}) bool {
	switch vv := a.(type) {
	case string:
		return strEqualsObject(vv, b)
	}

	switch vv := b.(type) {
	case string:
		return strEqualsObject(vv, a)
	}

	return reflect.DeepEqual(a, b)
}

func strEqualsObject(str string, i interface{}) bool {
	if i != nil {
		switch vv := i.(type) {
		case string:
			return vv == str
		case int64:
			if d, err := strconv.ParseInt(str, 10, 64); err == nil {
				return d == vv
			}
		case float64:
			if d, err := strconv.ParseFloat(str, 64); err == nil {
				return d == vv
			}
		case bool:
			if d, err := strconv.ParseBool(str); err == nil {
				return d == vv
			}
		case time.Time:
			if d, err := time.Parse(time.RFC3339, str); err == nil {
				return d == vv
			}
		case time.Duration:
			if d, err := time.ParseDuration(str); err == nil {
				return d == vv
			}
		case net.IP:
			if d := net.ParseIP(str); d != nil {
				return d.Equal(vv)
			}
		case EmailAddress:
			return string(vv) == str
		case URI:
			return string(vv) == str
		case DNSName:
			return string(vv) == str
		case map[string]string:
			// parse str representation of map and then compare
			m := make(map[string]string)
			for _, entry := range strings.Split(str, "&") {
				kvs := strings.Split(entry, "=")
				if len(kvs) == 2 {
					m[kvs[0]] = kvs[1]
				}
			}
			if len(m) == len(vv) {
				for k, v := range vv {
					if m[k] != v {
						return false
					}
				}
				return true
			}
			return false
		default:
			return Stringify(vv) == str
		}
	}

	// if i == nil and str is empty, return true
	return len(str) == 0
}

func serializeTime(t time.Time) []byte {
	t = t.UTC()
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	micros := t.Nanosecond() / 1000

	buf := make([]byte, 27)

	buf[0] = byte((year/1000)%10) + '0'
	buf[1] = byte((year/100)%10) + '0'
	buf[2] = byte((year/10)%10) + '0'
	buf[3] = byte(year%10) + '0'
	buf[4] = '-'
	buf[5] = byte((month)/10) + '0'
	buf[6] = byte((month)%10) + '0'
	buf[7] = '-'
	buf[8] = byte((day)/10) + '0'
	buf[9] = byte((day)%10) + '0'
	buf[10] = 'T'
	buf[11] = byte((hour)/10) + '0'
	buf[12] = byte((hour)%10) + '0'
	buf[13] = ':'
	buf[14] = byte((minute)/10) + '0'
	buf[15] = byte((minute)%10) + '0'
	buf[16] = ':'
	buf[17] = byte((second)/10) + '0'
	buf[18] = byte((second)%10) + '0'
	buf[19] = '.'
	buf[20] = byte((micros/100000)%10) + '0'
	buf[21] = byte((micros/10000)%10) + '0'
	buf[22] = byte((micros/1000)%10) + '0'
	buf[23] = byte((micros/100)%10) + '0'
	buf[24] = byte((micros/10)%10) + '0'
	buf[25] = byte((micros)%10) + '0'
	buf[26] = 'Z'
	return buf
}
