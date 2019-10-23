// Copyright 2018 Istio Authors
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

package v2_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	authn "istio.io/api/authentication/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/pilot/pkg/model"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/tests/util"
)

func TestSyncz(t *testing.T) {
	t.Run("return the sent and ack status of adsClient connections", func(t *testing.T) {
		_, tearDown := initLocalPilotTestEnv(t)
		defer tearDown()

		adsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()

		// Need to send two of each so that the second sends an Ack that is picked up
		if err := sendEDSReq([]string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarID(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendEDSReq([]string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarID(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendCDSReq(sidecarID(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendCDSReq(sidecarID(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendLDSReq(sidecarID(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendLDSReq(sidecarID(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			_, err := adsReceive(adsstr, 5*time.Second)
			if err != nil {
				t.Fatal("Recv failed", err)
			}
		}
		if err := sendRDSReq(sidecarID(app3Ip, "syncApp"), []string{"80", "8080"}, "", adsstr); err != nil {
			t.Fatal(err)
		}
		rdsResponse, err := adsReceive(adsstr, 5*time.Second)
		if err != nil {
			t.Fatal("Recv failed", err)
		}
		if err := sendRDSReq(sidecarID(app3Ip, "syncApp"), []string{"80", "8080"}, rdsResponse.Nonce, adsstr); err != nil {
			t.Fatal(err)
		}

		node, _ := model.ParseServiceNodeWithMetadata(sidecarID(app3Ip, "syncApp"), &model.NodeMetadata{})
		verifySyncStatus(t, node.ID, true, true)
	})
	t.Run("sync status not set when Nackd", func(t *testing.T) {
		_, tearDown := initLocalPilotTestEnv(t)
		defer tearDown()

		adsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()

		if err := sendEDSReq([]string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarID(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendEDSNack([]string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarID(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendCDSReq(sidecarID(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendCDSNack(sidecarID(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendLDSReq(sidecarID(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendLDSNack(sidecarID(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			_, err := adsReceive(adsstr, 5*time.Second)
			if err != nil {
				t.Fatal("Recv failed", err)
			}
		}
		if err := sendRDSReq(sidecarID(app3Ip, "syncApp2"), []string{"80", "8080"}, "", adsstr); err != nil {
			t.Fatal(err)
		}
		rdsResponse, err := adsReceive(adsstr, 5*time.Second)
		if err != nil {
			t.Fatal("Recv failed", err)
		}
		if err := sendRDSNack(sidecarID(app3Ip, "syncApp2"), []string{"80", "8080"}, rdsResponse.Nonce, adsstr); err != nil {
			t.Fatal(err)
		}
		node, _ := model.ParseServiceNodeWithMetadata(sidecarID(app3Ip, "syncApp2"), &model.NodeMetadata{})
		verifySyncStatus(t, node.ID, true, false)
	})
}

func getSyncStatus(t *testing.T) []v2.SyncStatus {
	req, err := http.NewRequest("GET", "/debug", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	syncz := http.HandlerFunc(v2.Syncz)
	syncz.ServeHTTP(rr, req)
	got := []v2.SyncStatus{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Error(err)
	}
	return got
}

func verifySyncStatus(t *testing.T, nodeID string, wantSent, wantAcked bool) {
	// This is a mostly horrible hack because the single pilot instance is shared across multiple tests
	// This makes this test contaminated by others and gives it horrible timing windows
	attempts := 5
	for i := 0; i < attempts; i++ {
		gotStatus := getSyncStatus(t)
		var errorHandler func(string, ...interface{})
		if i == attempts-1 {
			errorHandler = t.Errorf
		} else {
			errorHandler = t.Logf
		}
		for _, ss := range gotStatus {
			if ss.ProxyID == nodeID {
				if ss.ProxyVersion == "" {
					errorHandler("ProxyVersion should always be set for %v", nodeID)
				}
				if (ss.ClusterSent != "") != wantSent {
					errorHandler("wanted ClusterSent set %v got %v for %v", wantSent, ss.ClusterSent, nodeID)
				}
				if (ss.ClusterAcked != "") != wantAcked {
					errorHandler("wanted ClusterAcked set %v got %v for %v", wantAcked, ss.ClusterAcked, nodeID)
				}
				if (ss.ListenerSent != "") != wantSent {
					errorHandler("wanted ListenerSent set %v got %v for %v", wantSent, ss.ListenerSent, nodeID)
				}
				if (ss.ListenerAcked != "") != wantAcked {
					errorHandler("wanted ListenerAcked set %v got %v for %v", wantAcked, ss.ListenerAcked, nodeID)
				}
				if (ss.RouteSent != "") != wantSent {
					errorHandler("wanted RouteSent set %v got %v for %v", wantSent, ss.RouteSent, nodeID)
				}
				if (ss.RouteAcked != "") != wantAcked {
					errorHandler("wanted RouteAcked set %v got %v for %v", wantAcked, ss.RouteAcked, nodeID)
				}
				if (ss.EndpointSent != "") != wantSent {
					errorHandler("wanted EndpointSent set %v got %v for %v", wantSent, ss.EndpointSent, nodeID)
				}
				if (ss.EndpointAcked != "") != wantAcked {
					errorHandler("wanted EndpointAcked set %v got %v for %v", wantAcked, ss.EndpointAcked, nodeID)
				}
				if (ss.EndpointPercent != 0) != wantAcked {
					errorHandler("wanted EndpointPercent set %v got %v for %v", wantAcked, ss.EndpointPercent, nodeID)
				}
				return
			}
		}
		errorHandler("node id %v not found", nodeID)
	}
}

func TestConfigDump(t *testing.T) {
	tests := []struct {
		name     string
		wantCode int
		proxyID  string
	}{
		{
			name:     "dumps most recent proxy with 200",
			proxyID:  "dumpApp-644fc65469-96dza.testns",
			wantCode: 200,
		},
		{
			name:     "returns 404 if proxy not found",
			proxyID:  "not-found",
			wantCode: 404,
		},
		{
			name:     "returns 400 if no proxyID",
			proxyID:  "",
			wantCode: 400,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, tearDown := initLocalPilotTestEnv(t)
			defer tearDown()

			for i := 0; i < 2; i++ {
				envoy, cancel, err := connectADS(util.MockPilotGrpcAddr)
				if err != nil {
					t.Fatal(err)
				}
				defer cancel()
				if err := sendCDSReq(sidecarID(app3Ip, "dumpApp"), envoy); err != nil {
					t.Fatal(err)
				}
				if err := sendLDSReq(sidecarID(app3Ip, "dumpApp"), envoy); err != nil {
					t.Fatal(err)
				}
				// Only most recent proxy will have routes
				if i == 1 {
					if err := sendRDSReq(sidecarID(app3Ip, "dumpApp"), []string{"80", "8080"}, "", envoy); err != nil {
						t.Fatal(err)
					}
					_, err := adsReceive(envoy, 5*time.Second)
					if err != nil {
						t.Fatal("Recv failed", err)
					}
				}
				for j := 0; j < 2; j++ {
					_, err := adsReceive(envoy, 5*time.Second)
					if err != nil {
						t.Fatal("Recv failed", err)
					}
				}
			}
			wrapper := getConfigDump(t, s.EnvoyXdsServer, tt.proxyID, tt.wantCode)
			if wrapper != nil {
				if rs, err := wrapper.GetDynamicRouteDump(false); err != nil || len(rs.DynamicRouteConfigs) == 0 {
					t.Errorf("routes were present, must have received an older connection's dump")
				}
			} else if tt.wantCode < 400 {
				t.Error("expected a non-nil wrapper with successful status code")
			}
		})
	}
}

func getConfigDump(t *testing.T, s *v2.DiscoveryServer, proxyID string, wantCode int) *configdump.Wrapper {
	path := "/config_dump"
	if proxyID != "" {
		path += fmt.Sprintf("?proxyID=%v", proxyID)
	}
	req, err := http.NewRequest("GET", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	syncz := http.HandlerFunc(s.ConfigDump)
	syncz.ServeHTTP(rr, req)
	if rr.Code != wantCode {
		t.Errorf("wanted response code %v, got %v", wantCode, rr.Code)
	}
	if wantCode > 399 {
		return nil
	}
	got := &configdump.Wrapper{}
	if err := got.UnmarshalJSON(rr.Body.Bytes()); err != nil {
		t.Fatalf(err.Error())
	}
	return got
}

// TestAuthenticationZ tests the /debug/authenticationz handle. Due to the limitation of the test setup,
// this test converts only one simple scenario. See TestAnalyzeMTLSSettings for more
func TestAuthenticationZ(t *testing.T) {
	tests := []struct {
		name           string
		proxyID        string
		wantCode       int
		expectedLength int
	}{
		{
			name:           "returns 400 if proxyID not provided",
			proxyID:        "",
			wantCode:       400,
			expectedLength: 0,
		},
		{
			name:           "returns 404 if proxy not found",
			proxyID:        "not-found",
			wantCode:       404,
			expectedLength: 0,
		},
		{
			name:           "dumps most recent proxy with 200",
			proxyID:        "dumpApp-644fc65469-96dza.testns",
			wantCode:       200,
			expectedLength: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, tearDown := initLocalPilotTestEnv(t)
			defer tearDown()

			envoy, cancel, err := connectADS(util.MockPilotGrpcAddr)
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()

			// Make CDS/LDS/RDS requestst to make the proxy instance available.
			if err := sendCDSReq(sidecarID(app3Ip, "dumpApp"), envoy); err != nil {
				t.Fatal(err)
			}
			if err := sendLDSReq(sidecarID(app3Ip, "dumpApp"), envoy); err != nil {
				t.Fatal(err)
			}
			if err := sendRDSReq(sidecarID(app3Ip, "dumpApp"), []string{"80", "8080"}, "", envoy); err != nil {
				t.Fatal(err)
			}
			if _, err := adsReceive(envoy, 5*time.Second); err != nil {
				t.Fatal("Recv failed", err)
			}

			got := getAuthenticationZ(t, s.EnvoyXdsServer, tt.proxyID, tt.wantCode)

			if len(got) != tt.expectedLength {
				t.Errorf("AuthenticationZ should have %d entries, got %d", tt.expectedLength, len(got))
			}
			for _, info := range got {
				expectedStatus := "OK"
				if info.Host == "mymongodb.somedomain" {
					expectedStatus = "CONFLICT"
				}
				if info.TLSConflictStatus != expectedStatus {
					t.Errorf("want TLS conflict status %q, got %v", expectedStatus, info)
				}
			}
		})
	}
}

func getAuthenticationZ(t *testing.T, s *v2.DiscoveryServer, proxyID string, wantCode int) []v2.AuthenticationDebug {
	path := "/debug/authenticationz"
	if proxyID != "" {
		path += fmt.Sprintf("?proxyID=%v", proxyID)
	}
	req, err := http.NewRequest("GET", path, nil)
	if err != nil {
		t.Fatal(err)
	}

	got := []v2.AuthenticationDebug{}
	var returnCode int

	// Retry 20 times, with 500 ms interval, so up to 10s.
	for numTries := 0; numTries < 20; numTries++ {
		rr := httptest.NewRecorder()
		authenticationz := http.HandlerFunc(s.Authenticationz)
		authenticationz.ServeHTTP(rr, req)
		returnCode = rr.Code
		if rr.Code == wantCode {
			if rr.Code == 200 {
				if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
			}
			break
		}

		// It could be delay in ADS propagation, hence cause different return code for the
		// authenticationz. Wait 0.5s then retry.
		t.Logf("/authenticationz returns with error code %v:\n%v", rr.Code, rr.Body)
		time.Sleep(500 * time.Millisecond)
	}

	if returnCode != wantCode {
		t.Errorf("wanted response code %v, got %v", wantCode, returnCode)
	}

	return got
}

func TestEvaluateTLSState(t *testing.T) {
	testCases := []struct {
		name                        string
		client                      *networking.TLSSettings
		server                      authn_model.MutualTLSMode
		expected                    string
		expectedWithAutoMTLSEnabled string
	}{
		{
			name:                        "Auto with mTLS disable",
			client:                      nil,
			server:                      authn_model.MTLSDisable,
			expected:                    "OK",
			expectedWithAutoMTLSEnabled: "AUTO",
		},
		{
			name:                        "Auto with mTLS permissive",
			client:                      nil,
			server:                      authn_model.MTLSPermissive,
			expected:                    "OK",
			expectedWithAutoMTLSEnabled: "AUTO",
		},
		{
			name:                        "Auto with mTLS STRICT",
			client:                      nil,
			server:                      authn_model.MTLSStrict,
			expected:                    "OK",
			expectedWithAutoMTLSEnabled: "AUTO",
		},
		{
			name: "OK with mTLS STRICT",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_ISTIO_MUTUAL,
			},
			server:                      authn_model.MTLSStrict,
			expected:                    "OK",
			expectedWithAutoMTLSEnabled: "OK",
		},
		{
			name: "OK with mTLS DISABLE",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_DISABLE,
			},
			server:                      authn_model.MTLSDisable,
			expected:                    "OK",
			expectedWithAutoMTLSEnabled: "OK",
		},
		{
			name: "OK: plaintext with mTLS PERMISSIVE",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_DISABLE,
			},
			server:                      authn_model.MTLSPermissive,
			expected:                    "OK",
			expectedWithAutoMTLSEnabled: "OK",
		},
		{
			name: "OK: ISTIO_MUTUAL with mTLS PERMISSIVE",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_ISTIO_MUTUAL,
			},
			server:                      authn_model.MTLSPermissive,
			expected:                    "OK",
			expectedWithAutoMTLSEnabled: "OK",
		},
		{
			name: "Conflict: plaintext with mTLS STRICT",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_DISABLE,
			},
			server:                      authn_model.MTLSStrict,
			expected:                    "CONFLICT",
			expectedWithAutoMTLSEnabled: "CONFLICT",
		},
		{
			name: "Conflict: (custom) mTLS with mTLS STRICT",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_MUTUAL,
			},
			server:                      authn_model.MTLSStrict,
			expected:                    "CONFLICT",
			expectedWithAutoMTLSEnabled: "CONFLICT",
		},
		{
			name: "Conflict: TLS simple with mTLS STRICT",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_SIMPLE,
			},
			server:                      authn_model.MTLSStrict,
			expected:                    "CONFLICT",
			expectedWithAutoMTLSEnabled: "CONFLICT",
		},
		{
			name: "Conflict: TLS simple with mTLS PERMISSIVE",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_SIMPLE,
			},
			server:                      authn_model.MTLSPermissive,
			expected:                    "CONFLICT",
			expectedWithAutoMTLSEnabled: "CONFLICT",
		},
		{
			name: "Conflict: (custom) mTLS with mTLS PERMISSIVE",
			client: &networking.TLSSettings{
				Mode: networking.TLSSettings_SIMPLE,
			},
			server:                      authn_model.MTLSPermissive,
			expected:                    "CONFLICT",
			expectedWithAutoMTLSEnabled: "CONFLICT",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := v2.EvaluateTLSState(false, tc.client, tc.server); got != tc.expected {
				t.Errorf("EvaluateTLSState expected to be %q, got %q", tc.expected, got)
			}
			if got := v2.EvaluateTLSState(true, tc.client, tc.server); got != tc.expectedWithAutoMTLSEnabled {
				t.Errorf("EvaluateTLSState with autoMTLSEnable expected to be %q, got %q", tc.expectedWithAutoMTLSEnabled, got)
			}
		})
	}
}

func TestAnalyzeMTLSSettings(t *testing.T) {
	fakeConfigMeta := model.ConfigMeta{
		Name:      "foo",
		Namespace: "bar",
	}
	testCases := []struct {
		name            string
		autoMTLSEnabled bool
		authnPolicy     *authn.Policy
		authnMeta       *model.ConfigMeta
		destConfig      *model.Config
		expected        []*v2.AuthenticationDebug
	}{
		{
			name:            "No policy",
			autoMTLSEnabled: false,
			authnPolicy:     nil,
			authnMeta:       nil,
			destConfig:      nil,
			expected: []*v2.AuthenticationDebug{
				{
					Host:                     "foo.default",
					Port:                     8080,
					AuthenticationPolicyName: "-",
					DestinationRuleName:      "-",
					ServerProtocol:           "DISABLE",
					ClientProtocol:           "-",
					TLSConflictStatus:        "OK",
				},
			},
		},
		{
			name:            "No policy with autoMtl",
			autoMTLSEnabled: true,
			authnPolicy:     nil,
			authnMeta:       nil,
			destConfig:      nil,
			expected: []*v2.AuthenticationDebug{
				{
					Host:                     "foo.default",
					Port:                     8080,
					AuthenticationPolicyName: "-",
					DestinationRuleName:      "-",
					ServerProtocol:           "DISABLE",
					ClientProtocol:           "-",
					TLSConflictStatus:        "AUTO",
				},
			},
		},
		{
			name:            "No DR",
			autoMTLSEnabled: false,
			authnPolicy: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{
								Mode: authn.MutualTls_STRICT,
							},
						},
					},
				},
			},
			authnMeta:  &fakeConfigMeta,
			destConfig: nil,
			expected: []*v2.AuthenticationDebug{
				{
					Host:                     "foo.default",
					Port:                     8080,
					AuthenticationPolicyName: "bar/foo",
					DestinationRuleName:      "-",
					ServerProtocol:           "STRICT",
					ClientProtocol:           "-",
					TLSConflictStatus:        "OK",
				},
			},
		},
		{
			name:            "DR without TLS Settings",
			autoMTLSEnabled: false,
			authnPolicy: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{
								Mode: authn.MutualTls_STRICT,
							},
						},
					},
				},
			},
			authnMeta: &fakeConfigMeta,
			destConfig: &model.Config{
				ConfigMeta: model.ConfigMeta{
					Name:      "some-rule",
					Namespace: "default",
				},
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{},
				},
			},
			expected: []*v2.AuthenticationDebug{
				{
					Host:                     "foo.default",
					Port:                     8080,
					AuthenticationPolicyName: "bar/foo",
					DestinationRuleName:      "default/some-rule",
					ServerProtocol:           "STRICT",
					ClientProtocol:           "-",
					TLSConflictStatus:        "OK",
				},
			},
		},
		{
			name:            "DR with TLS Settings",
			autoMTLSEnabled: false,
			authnPolicy: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{
								Mode: authn.MutualTls_STRICT,
							},
						},
					},
				},
			},
			authnMeta: &fakeConfigMeta,
			destConfig: &model.Config{
				ConfigMeta: model.ConfigMeta{
					Name:      "some-rule",
					Namespace: "default",
				},
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						Tls: &networking.TLSSettings{
							Mode: networking.TLSSettings_DISABLE,
						},
					},
				},
			},
			expected: []*v2.AuthenticationDebug{
				{
					Host:                     "foo.default",
					Port:                     8080,
					AuthenticationPolicyName: "bar/foo",
					DestinationRuleName:      "default/some-rule",
					ServerProtocol:           "STRICT",
					ClientProtocol:           "DISABLE",
					TLSConflictStatus:        "CONFLICT",
				},
			},
		},
		{
			name:            "DR with port-specific TLS Settings",
			autoMTLSEnabled: false,
			authnPolicy: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{
								Mode: authn.MutualTls_STRICT,
							},
						},
					},
				},
			},
			authnMeta: &fakeConfigMeta,
			destConfig: &model.Config{
				ConfigMeta: model.ConfigMeta{
					Name:      "some-rule",
					Namespace: "default",
				},
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						Tls: &networking.TLSSettings{
							Mode: networking.TLSSettings_DISABLE,
						},
						PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
							{
								Port: &networking.PortSelector{
									Number: 8080,
								},
								Tls: &networking.TLSSettings{
									Mode: networking.TLSSettings_ISTIO_MUTUAL,
								},
							},
						},
					},
				},
			},
			expected: []*v2.AuthenticationDebug{
				{
					Host:                     "foo.default",
					Port:                     8080,
					AuthenticationPolicyName: "bar/foo",
					DestinationRuleName:      "default/some-rule",
					ServerProtocol:           "STRICT",
					ClientProtocol:           "ISTIO_MUTUAL",
					TLSConflictStatus:        "OK",
				},
			},
		},
		{
			name:            "DR with subset",
			autoMTLSEnabled: false,
			authnPolicy: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{
								Mode: authn.MutualTls_STRICT,
							},
						},
					},
				},
			},
			authnMeta: &fakeConfigMeta,
			destConfig: &model.Config{
				ConfigMeta: model.ConfigMeta{
					Name:      "some-rule",
					Namespace: "default",
				},
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						Tls: &networking.TLSSettings{
							Mode: networking.TLSSettings_DISABLE,
						},
						PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
							{
								Port: &networking.PortSelector{
									Number: 8080,
								},
								Tls: &networking.TLSSettings{
									Mode: networking.TLSSettings_ISTIO_MUTUAL,
								},
							},
						},
					},
					Subsets: []*networking.Subset{
						{
							Name:   "foobar",
							Labels: map[string]string{"foo": "bar"},
							TrafficPolicy: &networking.TrafficPolicy{
								PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
									{
										Port: &networking.PortSelector{
											Number: 8080,
										},
										Tls: &networking.TLSSettings{
											Mode: networking.TLSSettings_SIMPLE,
										},
									},
								},
							},
						},
					},
				},
			},
			expected: []*v2.AuthenticationDebug{
				{
					Host:                     "foo.default",
					Port:                     8080,
					AuthenticationPolicyName: "bar/foo",
					DestinationRuleName:      "default/some-rule",
					ServerProtocol:           "STRICT",
					ClientProtocol:           "ISTIO_MUTUAL",
					TLSConflictStatus:        "OK",
				},
				{
					Host:                     "foo.default|foobar",
					Port:                     8080,
					AuthenticationPolicyName: "bar/foo",
					DestinationRuleName:      "default/some-rule",
					ServerProtocol:           "STRICT",
					ClientProtocol:           "SIMPLE",
					TLSConflictStatus:        "CONFLICT",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			port := model.Port{
				Port: 8080,
			}
			if got := v2.AnalyzeMTLSSettings(
				tc.autoMTLSEnabled, host.Name("foo.default"), &port, tc.authnPolicy, tc.authnMeta, tc.destConfig); !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("EvaluateTLSState expected to be %+v, got %+v", tc.expected, got)
			}
		})
	}
}
