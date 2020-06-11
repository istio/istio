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

package env

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/golang/protobuf/jsonpb"

	// Import all XDS config types
	_ "istio.io/istio/pkg/config/xds"

	"istio.io/istio/pkg/envoy"
)

// TestSetup store data for a test.
type TestSetup struct {
	t     *testing.T
	epoch int
	ports *Ports

	envoy             envoy.Instance
	backend           *HTTPServer
	testName          uint16
	stress            bool
	noProxy           bool
	noBackend         bool
	disableHotRestart bool
	checkDict         bool
	silentlyStopProxy bool

	// EnvoyTemplate is the bootstrap config used by envoy.
	EnvoyTemplate string

	// EnvoyParams contain extra envoy parameters to pass in the CLI (cluster, node)
	EnvoyParams []string

	// EnvoyConfigOpt allows passing additional parameters to the EnvoyTemplate
	EnvoyConfigOpt map[string]interface{}

	// IstioSrc is the base directory of istio sources. May be set for finding testdata or
	// other files in the source tree
	IstioSrc string

	// IstioOut is the base output directory.
	IstioOut string

	// AccessLogPath is the access log path for Envoy
	AccessLogPath string

	// Dir is the working dir for envoy
	Dir string
}

// NewTestSetup creates a new test setup
// "name" has to be defined in ports.go
func NewTestSetup(name uint16, t *testing.T) *TestSetup {
	return &TestSetup{
		t:             t,
		ports:         NewPorts(name),
		testName:      name,
		AccessLogPath: "/tmp/envoy-access.log",
	}
}

// Ports get ports object
func (s *TestSetup) Ports() *Ports {
	return s.ports
}

// SDSPath gets SDS path. The path does not change after proxy restarts.
func (s *TestSetup) SDSPath() string {
	return fmt.Sprintf("/tmp/sdstestudspath.%v", s.ports.STSPort)
}

// JWTTokenPath gets JWT token path. The path does not change after proxy restarts.
func (s *TestSetup) JWTTokenPath() string {
	return fmt.Sprintf("/tmp/envoy-token-%v.jwt", s.ports.STSPort)
}

// CACertPath gets CA cert file path. The path does not change after proxy restarts.
func (s *TestSetup) CACertPath() string {
	return fmt.Sprintf("/tmp/ca-certificates-%v.crt", s.ports.STSPort)
}

// SetStress set the stress flag
func (s *TestSetup) SetStress(stress bool) {
	s.stress = stress
}

// SetCheckDict set the checkDict flag
func (s *TestSetup) SetCheckDict(checkDict bool) {
	s.checkDict = checkDict
}

// SilentlyStopProxy ignores errors when stop proxy
func (s *TestSetup) SilentlyStopProxy(silent bool) {
	s.silentlyStopProxy = silent
}

// SetDisableHotRestart sets whether disable the HotRestart feature of Envoy
func (s *TestSetup) SetDisableHotRestart(disable bool) {
	s.disableHotRestart = disable
}

// SetNoProxy set NoProxy flag
func (s *TestSetup) SetNoProxy(no bool) {
	s.noProxy = no
}

// SetNoBackend sets no backend flag
func (s *TestSetup) SetNoBackend(no bool) {
	s.noBackend = no
}

// SetUp setups Envoy and Backend server for test.
func (s *TestSetup) SetUp() error {
	var err error
	s.envoy, err = s.newEnvoy()
	if err != nil {
		log.Printf("unable to create Envoy %v", err)
		return err
	}

	err = startEnvoy(s.envoy)
	if err != nil {
		return err
	}

	if !s.noBackend {
		s.backend, err = NewHTTPServer(s.ports.BackendPort)
		if err != nil {
			log.Printf("unable to create HTTP server %v", err)
		} else {
			errCh := s.backend.Start()
			if err = <-errCh; err != nil {
				log.Fatalf("backend server start failed %v", err)
			}
		}
	}

	s.WaitEnvoyReady()

	return nil
}

// TearDown shutdown the servers.
func (s *TestSetup) TearDown() {
	if err := stopEnvoy(s.envoy); err != nil && !s.silentlyStopProxy {
		s.t.Errorf("error quitting envoy: %v", err)
	}
	removeEnvoySharedMemory(s.envoy)

	if s.backend != nil {
		s.backend.Stop()
	}
}

// LastRequestHeaders returns last backend request headers
func (s *TestSetup) LastRequestHeaders() http.Header {
	if s.backend != nil {
		return s.backend.LastRequestHeaders()
	}
	return nil
}

// ReStartEnvoy restarts Envoy
func (s *TestSetup) ReStartEnvoy() {
	// don't stop envoy before starting the new one since we use hot restart
	oldEnvoy := s.envoy
	s.ports = NewEnvoyPorts(s.ports, s.testName)
	log.Printf("new allocated ports are %v:", s.ports)
	var err error
	s.epoch++
	s.envoy, err = s.newEnvoy()
	if err != nil {
		s.t.Errorf("unable to re-start envoy %v", err)
		return
	}

	err = startEnvoy(s.envoy)
	if err != nil {
		s.t.Fatalf("unable to re-start envoy %v", err)
	}

	s.WaitEnvoyReady()

	_ = stopEnvoy(oldEnvoy)
}

// WaitForStatsUpdateAndGetStats waits for waitDuration seconds to let Envoy update stats, and sends
// request to Envoy for stats. Returns stats response.
func (s *TestSetup) WaitForStatsUpdateAndGetStats(waitDuration int) (string, error) {
	time.Sleep(time.Duration(waitDuration) * time.Second)
	statsURL := fmt.Sprintf("http://localhost:%d/stats?format=json&usedonly", s.Ports().AdminPort)
	code, respBody, err := HTTPGet(statsURL)
	if err != nil {
		return "", fmt.Errorf("sending stats request returns an error: %v", err)
	}
	if code != 200 {
		return "", fmt.Errorf("sending stats request returns unexpected status code: %d", code)
	}
	return respBody, nil
}

// GetStatsMap fetches Envoy stats with retry, and returns stats in a map.
func (s *TestSetup) GetStatsMap() (map[string]uint64, error) {
	delay := 200 * time.Millisecond
	total := 3 * time.Second
	var errGet error
	var code int
	var statsJSON string
	for attempt := 0; attempt < int(total/delay); attempt++ {
		statsURL := fmt.Sprintf("http://localhost:%d/stats?format=json&usedonly", s.Ports().AdminPort)
		code, statsJSON, errGet = HTTPGet(statsURL)
		if errGet != nil {
			log.Printf("sending stats request returns an error: %v", errGet)
		} else if code != 200 {
			log.Printf("sending stats request returns unexpected status code: %d", code)
		} else {
			return s.unmarshalStats(statsJSON), nil
		}
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("failed to get stats, err: %v, code: %d", errGet, code)
}

type statEntry struct {
	Name  string      `json:"name"`
	Value json.Number `json:"value"`
}

type stats struct {
	StatList []statEntry `json:"stats"`
}

// WaitEnvoyReady waits until envoy receives and applies all config
func (s *TestSetup) WaitEnvoyReady() {
	// Sometimes on CI, connection is refused even when envoy reports warm clusters and listeners...
	// Inject a 1 second delay to force readiness
	time.Sleep(1 * time.Second)

	delay := 200 * time.Millisecond
	total := 3 * time.Second
	var stats map[string]uint64
	for attempt := 0; attempt < int(total/delay); attempt++ {
		statsURL := fmt.Sprintf("http://localhost:%d/stats?format=json&usedonly", s.Ports().AdminPort)
		code, respBody, errGet := HTTPGet(statsURL)
		if errGet == nil && code == 200 {
			stats = s.unmarshalStats(respBody)
			warmingListeners, hasListeners := stats["listener_manager.total_listeners_warming"]
			warmingClusters, hasClusters := stats["cluster_manager.warming_clusters"]
			if hasListeners && hasClusters && warmingListeners == 0 && warmingClusters == 0 {
				return
			}
		}
		time.Sleep(delay)
	}

	s.t.Fatalf("envoy failed to get ready: %v", stats)
}

// UnmarshalStats Unmarshals Envoy stats from JSON format into a map, where stats name is
// key, and stats value is value.
func (s *TestSetup) unmarshalStats(statsJSON string) map[string]uint64 {
	statsMap := make(map[string]uint64)

	var statsArray stats
	if err := json.Unmarshal([]byte(statsJSON), &statsArray); err != nil {
		s.t.Fatalf("unable to unmarshal stats from json: %v", err)
	}

	for _, v := range statsArray.StatList {
		if v.Value == "" {
			continue
		}
		tmp, err := v.Value.Float64()
		if err != nil {
			s.t.Fatalf("unable to convert json.Number from stats: %v", err)
		}
		statsMap[v.Name] = uint64(tmp)
	}
	return statsMap
}

// VerifyStats verifies Envoy stats.
func (s *TestSetup) VerifyStats(expectedStats map[string]uint64) {
	s.t.Helper()

	check := func(actualStatsMap map[string]uint64) error {
		for eStatsName, eStatsValue := range expectedStats {
			aStatsValue, ok := actualStatsMap[eStatsName]
			if !ok && eStatsValue != 0 {
				return fmt.Errorf("failed to find expected stat %s", eStatsName)
			}
			if aStatsValue != eStatsValue {
				return fmt.Errorf("stats %s does not match. expected vs actual: %d vs %d",
					eStatsName, eStatsValue, aStatsValue)
			}

			log.Printf("stat %s is matched. value is %d", eStatsName, eStatsValue)
		}
		return nil
	}

	delay := 200 * time.Millisecond
	total := 3 * time.Second

	var err error
	for attempt := 0; attempt < int(total/delay); attempt++ {
		statsURL := fmt.Sprintf("http://localhost:%d/stats?format=json&usedonly", s.Ports().AdminPort)
		code, respBody, errGet := HTTPGet(statsURL)
		if errGet != nil {
			log.Printf("sending stats request returns an error: %v", errGet)
		} else if code != 200 {
			log.Printf("sending stats request returns unexpected status code: %d", code)
		} else {
			actualStatsMap := s.unmarshalStats(respBody)
			if err = check(actualStatsMap); err == nil {
				return
			}
			log.Printf("failed to verify stats: %v", err)
		}
		time.Sleep(delay)
	}
	s.t.Errorf("failed to find expected stats: %v", err)
}

// VerifyStatsLT verifies that Envoy stats contains stat expectedStat, whose value is less than
// expectedStatVal.
func (s *TestSetup) VerifyStatsLT(actualStats string, expectedStat string, expectedStatVal uint64) {
	s.t.Helper()
	actualStatsMap := s.unmarshalStats(actualStats)

	aStatsValue, ok := actualStatsMap[expectedStat]
	if !ok {
		s.t.Fatalf("Failed to find expected Stat %s\n", expectedStat)
	} else if aStatsValue >= expectedStatVal {
		s.t.Fatalf("Stat %s does not match. Expected value < %d, actual stat value is %d",
			expectedStat, expectedStatVal, aStatsValue)
	} else {
		log.Printf("stat %s is matched. %d < %d", expectedStat, aStatsValue, expectedStatVal)
	}
}

// go-control-plane requires v2 XDS types, when we are using v3 internally
// nolint: interfacer
func CastRouteToV2(r *route.RouteConfiguration) *v2.RouteConfiguration {
	s, err := (&jsonpb.Marshaler{OrigName: true}).MarshalToString(r)
	if err != nil {
		panic(err.Error())
	}
	v2route := &v2.RouteConfiguration{}
	err = jsonpb.UnmarshalString(s, v2route)
	if err != nil {
		panic(err.Error())
	}
	return v2route
}

// go-control-plane requires v2 XDS types, when we are using v3 internally
// nolint: interfacer
func CastListenerToV2(r *listener.Listener) *v2.Listener {
	s, err := (&jsonpb.Marshaler{OrigName: true}).MarshalToString(r)
	if err != nil {
		panic(err.Error())
	}
	v2Listener := &v2.Listener{}
	err = jsonpb.UnmarshalString(s, v2Listener)
	if err != nil {
		panic(err.Error())
	}
	return v2Listener
}
