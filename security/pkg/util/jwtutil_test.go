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

package util

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

var (
	// thirdPartyJwt is generated in a testing K8s cluster, using the "istio-token" projected volume.
	// Token is issued at 2020-04-04 22:13:54 Pacific Daylight time.
	// Expiration time is 2020-04-05 10:13:54 Pacific Daylight time.
	thirdPartyJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9." +
		"eyJhdWQiOlsieW9uZ2dhbmdsLWlzdGlvLTQuc3ZjLmlkLmdvb2ciXSwiZXhwIjoxNTg2MTA2ODM0LCJpYXQiOjE1" +
		"ODYwNjM2MzQsImlzcyI6Imh0dHBzOi8vY29udGFpbmVyLmdvb2dsZWFwaXMuY29tL3YxL3Byb2plY3RzL3lvbmdn" +
		"YW5nbC1pc3Rpby00L2xvY2F0aW9ucy91cy1jZW50cmFsMS1hL2NsdXN0ZXJzL2NsdXN0ZXItMyIsImt1YmVybmV0" +
		"ZXMuaW8iOnsibmFtZXNwYWNlIjoiZm9vIiwicG9kIjp7Im5hbWUiOiJodHRwYmluLTY0Nzc2YmY3OGQtanFsNWIi" +
		"LCJ1aWQiOiI5YWQ3NTcxYi03NjBhLTExZWEtODllNy00MjAxMGE4MDAxYzEifSwic2VydmljZWFjY291bnQiOnsi" +
		"bmFtZSI6Imh0dHBiaW4iLCJ1aWQiOiI5OWY2NWY1MC03NjBhLTExZWEtODllNy00MjAxMGE4MDAxYzEifX0sIm5i" +
		"ZiI6MTU4NjA2MzYzNCwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmZvbzpodHRwYmluIn0.XWSCdarBR0cx" +
		"MlVV5X9pvgI9m0lyO-17B45aBKJBIQjvjluKXqxnCuIeD3X2ItLkCUzmKUa3ftTjUUov1MJ89MdBngNfUP7IfwnD" +
		"2dBl7Jtju0-Ks7aTFOkgtoMYqNnQ1VSDTAOfNpdZVUnsR_oY8obXSQR_H4uMcaNOGED2RX5HLBWFlvymtn4JXuyg" +
		"_rpOrJ8dv-snrmO3LT9y-zaUnZqSceDC8skzStrJIRvsRkO8GEcoQd5VwDn-UVgOcqWb-S-vgSjdtwBsnGPXsh_I" +
		"NZCq3ftr0Qu8-IxsIjpMjhLmAGTH1bR324aqLTYhAXp6fk06Pe3T9stCY5acSeadKA"
	// firstPartyJwt is generated in a testing K8s cluster. It is the default service account JWT.
	// No expiration time.
	firstPartyJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9." +
		"eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9u" +
		"YW1lc3BhY2UiOiJmb28iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlY3JldC5uYW1lIjoiaHR0cGJp" +
		"bi10b2tlbi14cWRncCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUi" +
		"OiJodHRwYmluIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQudWlkIjoiOTlm" +
		"NjVmNTAtNzYwYS0xMWVhLTg5ZTctNDIwMTBhODAwMWMxIiwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmZv" +
		"bzpodHRwYmluIn0.4kIl9TRjXEw6DfhtR-LdpxsAlYjJgC6Ly1DY_rqYY4h0haxcXB3kYZ3b2He-3fqOBryz524W" +
		"KkZscZgvs5L-sApmvlqdUG61TMAl7josB0x4IMHm1NS995LNEaXiI4driffwfopvqc_z3lVKfbF9j-mBgnCepxz3" +
		"UyWo5irFa3qcwbOUB9kuuUNGBdtbFBN5yIYLpfa9E-MtTX_zJ9fQ9j2pi8Z4ljii0tEmPmRxokHkmG_xNJjUkxKU" +
		"WZf4bLDdCEjVFyshNae-FdxiUVyeyYorTYzwZZYQch9MJeedg4keKKUOvCCJUlKixd2qAe-H7r15RPmo4AU5O5YL" +
		"65xiNg"

	// oneAudString includes one `aud` claim "abc" of type string.
	oneAudString = "header.eyJhdWQiOiJhYmMiLCJleHAiOjQ3MzI5OTQ4MDEsImlhdCI6MTU3OTM5NDgwMSwiaXNzIjoidGVzdC1pc3N1ZXItMUBpc3Rpby5pbyIsInN1YiI6InN1Yi0xIn0.signature" // nolint: lll

	// twoAudList includes two `aud` claims ["abc", "xyz"] of type []string.
	twoAudList = "header.eyJhdWQiOlsiYWJjIiwieHl6Il0sImV4cCI6NDczMjk5NDgwMSwiaWF0IjoxNTc5Mzk0ODAxLCJpc3MiOiJ0ZXN0LWlzc3Vlci0xQGlzdGlvLmlvIiwic3ViIjoic3ViLTEifQ.signature" // nolint: lll
)

func TestIsJwtExpired(t *testing.T) {
	testCases := map[string]struct {
		jwt       string
		now       time.Time
		expResult bool
		expErr    error
	}{
		"Not expired JWT": {
			jwt:       thirdPartyJwt,
			now:       time.Date(2020, time.April, 5, 6, 0, 0, 0, time.FixedZone("UTC-7", 0)),
			expResult: false,
			expErr:    nil,
		},
		"Expired JWT": {
			jwt:       thirdPartyJwt,
			now:       time.Now(),
			expResult: true,
			expErr:    nil,
		},
		"JWT without expiration": {
			jwt:       firstPartyJwt,
			now:       time.Now(),
			expResult: false,
			expErr:    nil,
		},
		"Invalid JWT - wrong number of sections": {
			jwt:    "invalid-section1.invalid-section2",
			now:    time.Now(),
			expErr: fmt.Errorf("token contains an invalid number of segments: 2, expected: 3"),
		},
		"Invalid JWT - wrong encoding": {
			jwt:    "invalid-section1.invalid-section2.invalid-section3",
			now:    time.Now(),
			expErr: fmt.Errorf("failed to decode the JWT claims"),
		},
	}

	for id, tc := range testCases {
		expired, err := IsJwtExpired(tc.jwt, tc.now)
		if err != nil && tc.expErr == nil || err == nil && tc.expErr != nil {
			t.Errorf("%s: Got error \"%v\" not matching expected error \"%v\"", id, err, tc.expErr)
		} else if err != nil && tc.expErr != nil && err.Error() != tc.expErr.Error() {
			t.Errorf("%s: Got error \"%v\" not matching expected error \"%v\"", id, err, tc.expErr)
		} else if err == nil && expired != tc.expResult {
			t.Errorf("%s: Got expiration %v not matching expected expiration %v", id, expired, tc.expResult)
		}
	}
}

func TestGetAud(t *testing.T) {
	testCases := map[string]struct {
		jwt string
		aud []string
	}{
		"no audience": {
			jwt: firstPartyJwt,
		},
		"one audience string": {
			jwt: oneAudString,
			aud: []string{"abc"},
		},
		"one audience list": {
			jwt: thirdPartyJwt,
			aud: []string{"yonggangl-istio-4.svc.id.goog"},
		},
		"two audiences list": {
			jwt: twoAudList,
			aud: []string{"abc", "xyz"},
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			if got, _ := GetAud(tc.jwt); !reflect.DeepEqual(tc.aud, got) {
				t.Errorf("want audience %v but got %v", tc.aud, got)
			}
		})
	}
}

func Test3p(t *testing.T) {
	// Real token from a cluster, from
	///var/run/secrets/kubernetes.io/serviceaccount/token
	t1p := "eyJhbGciOiJSUzI1NiIsImtpZCI6ImFrSDdOSy14VHBXcUVZUGRJMUdRRXQ3dFJFeXhIN3JsNHdHWjFmX2Z6Qk0ifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJmb3J0aW8tYXNtIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6ImRlZmF1bHQtdG9rZW4tdmo5cDkiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoiZGVmYXVsdCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjNmNWQ1YzRmLTBlMTYtNGMwYy05MzM5LTNkZjcwN2Q0N2UyYyIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpmb3J0aW8tYXNtOmRlZmF1bHQifQ.Z8ZdK4m75BcQUUXVjrQqKcWyCPPjlVHh_Q4Az6OUqQIQp4Nc5z-cGa5BUhzEFsdzO1VZgvJo17Kn5or-mW5cCGjTQJio2voV_mq66DyoFLM53OZ-drOzWrc1S7Ma_mq5SsTsduYwDPgq49gGx-1etGZ9qEiyzMw0f8XswmAO3wUuMqfKiLpnBnu3keIeHkYfW9jy7hZrnbkUhDqnapJrBqBQoGIM2s3MdkS0s1HOQYZbObNvqDDnjXEzZwKiQMrppo2VkzAOUBxUobgRtpPVK1_uQdCyU75p6qKjkyx5tLrCP6DaV92BJOEOKT82qUT7e_EIv44XqlPQMAgGMyUM9g" // nolint: lll
	// Real token from /var/run/secrets/tokens/istio-token
	t3p := "eyJhbGciOiJSUzI1NiIsImtpZCI6ImFrSDdOSy14VHBXcUVZUGRJMUdRRXQ3dFJFeXhIN3JsNHdHWjFmX2Z6Qk0ifQ.eyJhdWQiOlsiY29zdGluLWFzbTEuc3ZjLmlkLmdvb2ciXSwiZXhwIjoxNTk3NTQ2MTYyLCJpYXQiOjE1OTc1MDI5NjIsImlzcyI6Imh0dHBzOi8vY29udGFpbmVyLmdvb2dsZWFwaXMuY29tL3YxL3Byb2plY3RzL2Nvc3Rpbi1hc20xL2xvY2F0aW9ucy91cy1jZW50cmFsMS1jL2NsdXN0ZXJzL2JpZzEiLCJrdWJlcm5ldGVzLmlvIjp7Im5hbWVzcGFjZSI6ImZvcnRpby1hc20iLCJwb2QiOnsibmFtZSI6ImZvcnRpby04OWY2Y2ZkNmYtOXJqNjQiLCJ1aWQiOiJmNzY1NjE4YS01NzY2LTRiOTctYjVlYi00MmYxZDkwMzU5MzEifSwic2VydmljZWFjY291bnQiOnsibmFtZSI6ImRlZmF1bHQiLCJ1aWQiOiIzZjVkNWM0Zi0wZTE2LTRjMGMtOTMzOS0zZGY3MDdkNDdlMmMifX0sIm5iZiI6MTU5NzUwMjk2Miwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmZvcnRpby1hc206ZGVmYXVsdCJ9.SkeGuKMlCq9e9O8fG0sd2nzJH_6-_ccioBElUSXtGiz84Myvi_UeTXTlxOy3SCrlepHQtBXQ38CecixOawB8RQitaIf1TgaHEye_ASkQ7Ei91O7OKxGjYqAyYRlpf5S72njZ2hmZZUJUgWjwsUXXWcsCfNu3jbr81V8v0fzxg8um-3iID8DzmEpgEhrUvM9rIFj5HwWkzvZ6ZLwQ3q8shiP21vSVsQh-wjV3zoK3ylymW_1v8hQt-2XzB11q0Hsm0W1PgxKyuw0DN9wGoInKiWmU1hEg8Vwi-2O6nqHcTfJ8P-0pM56MdqH_HJI66Ql8sjSazAF_aXSAZf1-RuPosg" // nolint: lll

	for _, s := range []string{t3p, "InvalidToken"} {
		if IsK8SUnbound(s) {
			t.Error("Expecting bound token, detected unbound ", s)
		}
	}
	if !IsK8SUnbound(t1p) {
		t.Error("Expecting unbound, detected bound ", t1p)
	}
}
