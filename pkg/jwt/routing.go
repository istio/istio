// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jwt

import (
	"strings"
)

// HeaderJWTClaim is the special header name used in virtual service for routing based on JWT claims.
const HeaderJWTClaim = "@request.auth.claims"

type Separator int

const (
	Dot Separator = iota
	Square
)

type RoutingClaim struct {
	Match     bool
	Separator Separator
	Claims    []string
}

func ToRoutingClaim(headerName string) RoutingClaim {
	rc := RoutingClaim{}
	if !strings.HasPrefix(strings.ToLower(headerName), HeaderJWTClaim) {
		return rc
	}

	name := headerName[len(HeaderJWTClaim):]
	if strings.HasPrefix(name, ".") && len(name) > 1 {
		// using `.` as a separator
		rc.Match = true
		rc.Separator = Dot
		rc.Claims = strings.Split(name[1:], ".")
	} else if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") && len(name) > 2 {
		// using `[]` as a separator
		rc.Match = true
		rc.Separator = Square
		rc.Claims = strings.Split(name[1:len(name)-1], "][")
	}

	return rc
}
