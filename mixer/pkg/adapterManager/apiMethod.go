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

package adapterManager

// apiMethod constants are used to refer to the methods handled by api.Handler
type apiMethod int

// Supported API methods
const (
	checkMethod apiMethod = iota
	reportMethod
	quotaMethod
)

// Name of all support API methods
const (
	checkMethodName  = "Check"
	reportMethodName = "Report"
	quotaMethodName  = "Quota"
)

var apiMethodToString = map[apiMethod]string{
	checkMethod:  checkMethodName,
	reportMethod: reportMethodName,
	quotaMethod:  quotaMethodName,
}

// String returns the string representation of the method, or "" if an unknown method is given.
func (a apiMethod) String() string {
	return apiMethodToString[a]
}
