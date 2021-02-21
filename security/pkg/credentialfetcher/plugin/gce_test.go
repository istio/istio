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

package plugin

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util/leak"
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
)

func TestShouldRotate(t *testing.T) {
	jwtExp := time.Date(2020, time.April, 5, 10, 13, 54, 0, time.FixedZone("PDT", -int((7*time.Hour).Seconds())))
	testCases := map[string]struct {
		jwt            string
		now            time.Time
		expectedRotate bool
	}{
		"remaining life time is in grace period": {
			jwt:            thirdPartyJwt,
			now:            jwtExp.Add(time.Duration(-10) * time.Minute),
			expectedRotate: true,
		},
		"remaining life time is not in grace period": {
			jwt:            thirdPartyJwt,
			now:            jwtExp.Add(time.Duration(-30) * time.Minute),
			expectedRotate: false,
		},
		"no cached credential": {
			now:            time.Now(),
			expectedRotate: true,
		},
		"no expiration in token": {
			jwt:            firstPartyJwt,
			now:            time.Now(),
			expectedRotate: true,
		},
		"invalid token": {
			jwt:            "invalid-token",
			now:            time.Now(),
			expectedRotate: true,
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			p := GCEPlugin{
				tokenCache: tc.jwt,
			}
			if rotate := p.shouldRotate(tc.now); rotate != tc.expectedRotate {
				t.Errorf("%s, shouldRotate(%s)=%t, expected %t",
					id, tc.now.String(), rotate, tc.expectedRotate)
			}
		})
	}
}

func creatJWTFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

func getJWTFromFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func getTokenFromServer(t *testing.T, p *GCEPlugin, callReps int) ([]string, []error) {
	t.Helper()
	var tokens []string
	var errs []error
	for i := 0; i < callReps; i++ {
		token, err := p.GetPlatformCredential()
		tokens = append(tokens, token)
		errs = append(errs, err)
	}
	return tokens, errs
}

func verifyError(t *testing.T, id string, errs []error, expectedErr error) {
	t.Helper()
	for _, err := range errs {
		if err == nil && expectedErr != nil || err != nil && expectedErr == nil {
			t.Errorf("%s, GetPlatformCredential() returns err: %v, want: %v", id, err, expectedErr)
		} else if err != nil && expectedErr != nil && err.Error() != expectedErr.Error() {
			t.Errorf("%s, GetPlatformCredential() returns err: %v, want: %v", id, err, expectedErr)
		}
	}
}

func verifyToken(t *testing.T, id, jwtPath string, tokens []string, expectedToken string) {
	t.Helper()
	for i, token := range tokens {
		// if expectedToken is not set, mock metadata server returns an auto-generated fake token.
		wantToken := fmt.Sprintf("%s%d", fakeTokenPrefix, i+1)
		if expectedToken != "" {
			wantToken = expectedToken
		}
		if token != wantToken {
			t.Errorf("%s, #%d call to GetPlatformCredential() returns token: %s, want: %s",
				id, i, token, wantToken)
		}
		if jwtPath != "" && i+1 == len(tokens) {
			// If this is the last round, and JWT path is set, check JWT in the file.
			jwtFile, err := getJWTFromFile(jwtPath)
			if err != nil {
				t.Fatalf("%s, failed to read token from file %s: %v", id, jwtPath, err)
			}
			if jwtFile != wantToken {
				t.Errorf("%s, %s has token %s, want %s", id, jwtPath, jwtFile, wantToken)
			}
		}
	}
}

func TestGCEPlugin(t *testing.T) {
	testCases := map[string]struct {
		jwt           string
		jwtPath       string
		expectedToken string
		expectedCall  int
		expectedErr   error
	}{
		"get VM credential": {
			jwt:           thirdPartyJwt,
			jwtPath:       fmt.Sprintf("/tmp/security-pkg-credentialfetcher-plugin-gcetest-%s", uuid.New().String()),
			expectedToken: thirdPartyJwt,
			expectedCall:  1,
		},
		"jwt path not set": {
			jwt:          thirdPartyJwt,
			expectedCall: 1,
			expectedErr:  fmt.Errorf("jwtPath is unset"),
		},
		"fetch credential multiple times": {
			expectedCall: 5,
			jwtPath:      fmt.Sprintf("/tmp/security-pkg-credentialfetcher-plugin-gcetest-%s", uuid.New().String()),
		},
	}

	SetTokenRotation(false)
	ms, err := StartMetadataServer()
	if err != nil {
		t.Fatalf("StartMetadataServer() returns err: %v", err)
	}
	t.Cleanup(func() {
		ms.Stop()
		SetTokenRotation(true)
	})

	for id, tc := range testCases {
		p := GCEPlugin{
			tokenCache: tc.jwt,
			jwtPath:    tc.jwtPath,
		}
		if err := creatJWTFile(tc.jwtPath); err != nil {
			t.Fatalf("%s, creatJWTFile() returns err: %v", id, err)
		}
		ms.Reset()
		ms.setToken(tc.jwt)

		tokens, errs := getTokenFromServer(t, &p, tc.expectedCall)

		verifyError(t, id, errs, tc.expectedErr)
		if tc.expectedErr == nil {
			if ms.NumGetTokenCall() != tc.expectedCall {
				t.Errorf("%s, metadata server receives %d calls, want %d",
					id, ms.NumGetTokenCall(), tc.expectedCall)
			}
			if tc.jwtPath != "" {
				verifyToken(t, id, tc.jwtPath, tokens, tc.expectedToken)
			}
		}
	}
}

func TestTokenRotationJob(t *testing.T) {
	testCases := map[string]struct {
		jwt           string
		jwtPath       string
		expectedToken string
		expectedCall  int
	}{
		// mock metadata server returns an expired token, that forces rotation job
		// to fetch new token during each rotation.
		"expired token needs rotation": {
			jwt:           thirdPartyJwt,
			jwtPath:       fmt.Sprintf("/tmp/security-pkg-credentialfetcher-plugin-gcetest-%s", uuid.New().String()),
			expectedToken: thirdPartyJwt,
			expectedCall:  3,
		},
		// mock metadata server returns a token which has no exp field, that forces rotation job
		// to fetch new token during each rotation.
		"token with no expiration time needs rotation": {
			jwt:           firstPartyJwt,
			jwtPath:       fmt.Sprintf("/tmp/security-pkg-credentialfetcher-plugin-gcetest-%s", uuid.New().String()),
			expectedToken: firstPartyJwt,
			expectedCall:  3,
		},
		// mock metadata server returns an invalid token, that forces rotation job
		// to fetch new token during each rotation.
		"invalid token needs rotation": {
			jwt:           "invalid-token-section-1.invalid-token-section-2",
			jwtPath:       fmt.Sprintf("/tmp/security-pkg-credentialfetcher-plugin-gcetest-%s", uuid.New().String()),
			expectedCall:  3,
			expectedToken: "invalid-token-section-1.invalid-token-section-2",
		},
	}

	// starts rotation job every 0.5 second.
	rotationInterval = 500 * time.Millisecond
	SetTokenRotation(true)
	ms, err := StartMetadataServer()
	if err != nil {
		t.Fatalf("StartMetadataServer() returns err: %v", err)
	}
	t.Cleanup(func() {
		ms.Stop()
		rotationInterval = 5 * time.Minute
	})

	for id, tc := range testCases {
		// These tests should not run in parallel because they share one metadata server.
		t.Run(id, func(t *testing.T) {
			p := CreateGCEPlugin("", tc.jwtPath, "")
			if err := creatJWTFile(tc.jwtPath); err != nil {
				t.Fatalf("%s, creatJWTFile() returns err: %v", id, err)
			}
			ms.Reset()
			ms.setToken(tc.jwt)

			// Verify that rotation job is kicked multiple times.
			retryTimeout := time.Duration(2+tc.expectedCall) * rotationInterval
			retry.UntilSuccessOrFail(t, func() error {
				callNumber := ms.NumGetTokenCall()
				if callNumber < tc.expectedCall {
					return fmt.Errorf("%s, got %d token fetch calls, expected %d",
						id, callNumber, tc.expectedCall)
				}
				return nil
			}, retry.Delay(time.Second), retry.Timeout(retryTimeout))

			p.Stop()
		})
	}
}

func TestMain(m *testing.M) {
	leak.CheckMain(m)
}
