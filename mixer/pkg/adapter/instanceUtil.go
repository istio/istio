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

package adapter

import (
	"fmt"
	"net"
	"strconv"
	"time"
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
		case time.Time:
			return vv.String()
		case time.Duration:
			return vv.String()
		case net.IP:
			return vv.String()
		case EmailAddress:
			return string(vv)
		case URI:
			return string(vv)
		case DNSName:
			return string(vv)
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// StringEquals compares if string representations of a and b are equal.
// Note: string representation of nil object is an empty string, so StringEquals with
// a nil object and a string of value empty "", will evaluate to true.
func StringEquals(a interface{}, b interface{}) bool {
	if a == b {
		return true
	}
	return Stringify(a) == Stringify(b)
}
