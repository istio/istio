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

package cache

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
)

func constructCSRHostName(trustDomain, token string) (string, error) {
	// If token is jwt format, construct host name from jwt with format like spiffe://cluster.local/ns/foo/sa/sleep,
	strs := strings.Split(token, ".")
	if len(strs) != 3 {
		return "", fmt.Errorf("invalid k8s jwt token")
	}

	payload := strs[1]
	if l := len(payload) % 4; l > 0 {
		payload += strings.Repeat("=", 4-l)
	}
	dp, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("invalid k8s jwt token: %v", err)
	}

	var jp k8sJwtPayload
	if err = json.Unmarshal(dp, &jp); err != nil {
		return "", fmt.Errorf("invalid k8s jwt token: %v", err)
	}

	// sub field in jwt should be in format like: system:serviceaccount:foo:bar
	ss := strings.Split(jp.Sub, ":")
	if len(ss) != 4 {
		return "", fmt.Errorf("invalid sub field in k8s jwt token")
	}
	ns := ss[2] //namespace
	sa := ss[3] //service account

	domain := "cluster.local"
	if trustDomain != "" {
		domain = trustDomain
	}

	return fmt.Sprintf(identityTemplate, domain, ns, sa), nil
}

// isRetryableErr checks if a failed request should be retry based on gRPC resp code or http status code.
func isRetryableErr(c codes.Code, httpRespCode int, isGrpc bool) bool {
	if isGrpc {
		switch c {
		case codes.Canceled, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal, codes.Unavailable:
			return true
		}
	} else if httpRespCode >= 500 && !(httpRespCode == 501 || httpRespCode == 505 || httpRespCode == 511) {
		return true
	}
	return false
}

// cacheLogPrefix returns a unified log prefix.
func cacheLogPrefix(resourceName string) string {
	lPrefix := fmt.Sprintf("resource:%s", resourceName)
	return lPrefix
}

// cacheLogPrefix returns a unified log prefix.
func cacheLogPrefixWithReqID(resourceName, reqID string) string {
	lPrefix := fmt.Sprintf("resource:%s request:%s", resourceName, reqID)
	return lPrefix
}
