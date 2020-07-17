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

package client_test

// JwtAuthConfig is the jwt-auth envoy config
// nolint
const JwtAuthConfig = `
- name: jwt-auth
  typed_config:
    '@type': type.googleapis.com/udpa.type.v1.TypedStruct
    type_url: "type.googleapis.com/envoy.extensions.filters.http.jwt_authn.v3.JwtAuthentication"
    value:
      providers:
        test:
          issuer: issuer@foo.com
          audiences:
          - aud1
          local_jwks:
            inline_string: '{ "keys" : [ {"e":   "AQAB", "kid": "DHFbpoIUqrY8t2zpA2qXfCmr5VO5ZEr4RzHU_-envvQ", "kty": "RSA","n":   "xAE7eB6qugXyCAG3yhh7pkDkT65pHymX-P7KfIupjf59vsdo91bSP9C8H07pSAGQO1MV_xFj9VswgsCg4R6otmg5PV2He95lZdHtOcU5DXIg_pbhLdKXbi66GlVeK6ABZOUW3WYtnNHD-91gVuoeJT_DwtGGcp4ignkgXfkiEm4sw-4sfb4qdt5oLbyVpmW6x9cfa7vs2WTfURiCrBoUqgBo_-4WTiULmmHSGZHOjzwa8WtrtOQGsAFjIbno85jp6MnGGGZPYZbDAa_b3y5u-YpW7ypZrvD8BgtKVjgtQgZhLAGezMt0ua3DRrWnKqTZ0BJ_EyxOGuHJrLsn00fnMQ"}]}'
          forward_payload_header: test-jwt-payload-output
          payload_in_metadata: issuer@foo.com
      rules:    
      - match:
          prefix: /
        requires:
          provider_name: test
`

// JwtTestToken is formed from the following value
// {
//    "aud":"aud1",
//   "exp":20000000000,
//   "iat":1500000000,
//   "iss":"issuer@foo.com",
//   "some-other-string-claims":"some-claims-kept",
//   "sub":"sub@foo.com"
// }
const JwtTestToken = "eyJhbGciOiJSUzI1NiIsImtpZCI6IkRIRmJwb0lVcXJZOHQyenBBMnFYZ" +
	"kNtcjVWTzVaRXI0UnpIVV8tZW52dlEiLCJ0eXAiOiJKV1QifQ.eyJhdWQ" +
	"iOiJhdWQxIiwiZXhwIjoyMDAwMDAwMDAwMCwiaWF0IjoxNTAwMDAwMDAw" +
	"LCJpc3MiOiJpc3N1ZXJAZm9vLmNvbSIsInNvbWUtb3RoZXItc3RyaW5nL" +
	"WNsYWltcyI6InNvbWUtY2xhaW1zLWtlcHQiLCJzdWIiOiJzdWJAZm9vLm" +
	"NvbSJ9.VYQdAqzlzpVBoKQMkmwm4oCX-wgMieR7rEpJiOggYocEJbEINr" +
	"ZSMas9bJ0CQXdv5UWR6NiO-p1Ko1Zol1X5Ma93Aego18vygY1K1bZ5whX" +
	"qVtbkpDe5tUaPNP58uKWsh8g3EA2Mpr1jF7RgGCYmiW_LlWJnLlBMEvbb" +
	"pkBFy43Yfzn_wpLHNBTO8cUGHGMErBeBSe2jUYmdOda1s51rGmS-CuQDL" +
	"GMeJPmc2l50AOO0tnNbSp3S3KfeyF918uDFfDRLYp7j16cx71ETXfLsrX" +
	"UkcLOLthIYGpuD0RgvLi5soHDpV_uNO8FDiOPMs8y60EUQUcuSKZZHTS_" +
	"hzONkhg"
