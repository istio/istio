// Copyright 2020 Istio Authors
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
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/util/retry"
)

var (
	// thirdPartyJwt is generated in a testing K8s cluster, using the "istio-token" projected volume.
	// Expiration time is 2020-04-04 22:13:54.
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
	// newFirstPartyJwt is generated in a testing K8s cluster. It is the default service account JWT.
	// No expiration time.
	newFirstPartyJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IlNsQ282WGxzUmtUZFFlTlFjb3ZCaTI3RmpPTFRuS1NsM" +
		"EdwS0luLVFMdlEifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZX" +
		"J2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJpc3Rpby1zeXN0ZW0iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW" +
		"50L3NlY3JldC5uYW1lIjoiaXN0aW8tcGlsb3Qtc2VydmljZS1hY2NvdW50LXRva2VuLXQ3NnJnIiwia3ViZXJuZX" +
		"Rlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQubmFtZSI6ImlzdGlvLXBpbG90LXNlcnZpY2UtYW" +
		"Njb3VudCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjBjNDYyND" +
		"ZmLWRkZGItNDk2MS05YjUxLTAyMzg3MGMxNjkzZSIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDppc3Rpby" +
		"1zeXN0ZW06aXN0aW8tcGlsb3Qtc2VydmljZS1hY2NvdW50In0.xXm2vr1dR2qyrAdBqHJqa_VDuBV8mOe7jiRwG1" +
		"rZl0aMRtYF--DOMd4hGfaK-I-GieYP6TNzFrkqrT0DLGUOMiBnzb60PM103xEfuuFFlkxA4IlJgN2_e-vB5QO2Qh" +
		"LghxSI7up4xFBxvfTN5TXGmTdnnlFw4XVKRrM1Vgl8YlVyL6xc9CC4ckfF0SMNNyMOQk3v5gBwAzImdFw198YLDx" +
		"rNzXMfAPQ_PnJYFBXnin-BrGVEd2HknamUuG9XNvBhIpI-o-oXgUEEEm_zTQd_qbpOCZd0utvKhknTzjb92kUVRj" +
		"xG13vscVKNHqBVOYu1M69uHlx53Bt3o903oAB6rA"
)

func TestJwtLoaderWithFileContentUpdate(t *testing.T) {
	jwtFile, err := ioutil.TempFile("", "jwt")
	if err != nil {
		t.Fatalf("Failed to create tmp jwt file: %v", err)
	}
	defer os.Remove(jwtFile.Name()) // clean up

	if _, err := jwtFile.Write([]byte(firstPartyJwt)); err != nil {
		t.Fatalf("Failed to write jwt to tmp file: %v", err)
	}

	jwtLoader, err := NewJwtLoader(jwtFile.Name())
	if err != nil {
		t.Fatalf("Failed to create JWT loader: %v", err)
	}
	stopCh := make(chan struct{})
	go jwtLoader.Run(stopCh)
	defer close(stopCh)

	if jwtLoader.Jwt != firstPartyJwt {
		t.Errorf("Unexpected JWT loaded: '%s', expected '%s'", jwtLoader.Jwt, firstPartyJwt)
	}

	if err := jwtFile.Truncate(0); err != nil {
		t.Fatalf("Failed to wipe out the existing content in file: %v", err)
	}
	// Go to the origin of the file
	if _, err := jwtFile.Seek(0, 0); err != nil {
		t.Fatalf("Failed to seek in file: %v", err)
	}
	// Update the JWT file with newFirstPrtyJwt
	if _, err := jwtFile.Write([]byte(newFirstPartyJwt)); err != nil {
		t.Fatalf("Failed to update jwt to tmp file: %v", err)
	}

	retry.UntilSuccessOrFail(t, func() error {
		if strings.Compare(jwtLoader.Jwt, newFirstPartyJwt) != 0 {
			return fmt.Errorf("updated JWT file not loaded")
		}
		return nil
	}, retry.Delay(time.Millisecond*500), retry.Timeout(time.Second*3))
}

func TestJwtLoaderWithFileRecreation(t *testing.T) {
	jwtFile, err := ioutil.TempFile("", "jwt")
	fileName := jwtFile.Name()
	if err != nil {
		t.Fatalf("Failed to create tmp jwt file: %v", err)
	}
	defer os.Remove(fileName)

	if _, err := jwtFile.Write([]byte(firstPartyJwt)); err != nil {
		t.Fatalf("Failed to write jwt to tmp file: %v", err)
	}

	jwtLoader, err := NewJwtLoader(jwtFile.Name())
	if err != nil {
		t.Fatalf("Failed to create JWT loader: %v", err)
	}
	stopCh := make(chan struct{})
	go jwtLoader.Run(stopCh)
	defer close(stopCh)

	if jwtLoader.Jwt != firstPartyJwt {
		t.Errorf("Unexpected JWT loaded: '%s', expected '%s'", jwtLoader.Jwt, firstPartyJwt)
	}
	jwtFile.Close()
	os.Remove(fileName)

	// Recreate the file.
	jwtFile, err = os.Create(fileName)
	if err != nil {
		t.Fatalf("Failed to create tmp jwt file: %v", err)
	}
	// Update the file content
	if _, err := jwtFile.Write([]byte(newFirstPartyJwt)); err != nil {
		t.Fatalf("Failed to update jwt to tmp file: %v", err)
	}

	retry.UntilSuccessOrFail(t, func() error {
		if strings.Compare(jwtLoader.Jwt, newFirstPartyJwt) != 0 {
			return fmt.Errorf("updated JWT file not loaded")
		}
		return nil
	}, retry.Delay(time.Millisecond*500), retry.Timeout(time.Second*3))
}

func TestIsJwtExpired(t *testing.T) {
	testCases := map[string]struct {
		jwt       string
		now       time.Time
		expResult bool
		expErr    error
	}{
		"Not expired JWT": {
			jwt:       thirdPartyJwt,
			now:       time.Date(2019, time.November, 10, 23, 0, 0, 0, time.UTC),
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
