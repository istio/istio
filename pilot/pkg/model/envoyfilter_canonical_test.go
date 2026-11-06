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

package model

import (
	"bytes"
	"testing"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// buildHCMWithJSONAccessLog mimics the shape of a real EnvoyFilter patch: an
// HTTP connection manager whose access log config is carried in a nested Any, and whose
// log format is a Struct - that is, a Go map, the thing whose iteration order is random.
func buildHCMWithJSONAccessLog(t *testing.T, names []string) *hcm.HttpConnectionManager {
	t.Helper()
	fields := make(map[string]interface{}, len(names))
	for _, n := range names {
		fields[n] = "%" + n + "%"
	}
	st, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("failed to build struct: %v", err)
	}
	fl := &fileaccesslog.FileAccessLog{
		Path: "/var/log/x.log",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{JsonFormat: st},
			},
		},
	}
	anyLog, err := anypb.New(fl)
	if err != nil {
		t.Fatalf("failed to pack access log: %v", err)
	}
	return &hcm.HttpConnectionManager{
		AccessLog: []*accesslog.AccessLog{{
			ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: anyLog},
		}},
	}
}

func accessLogPayload(t *testing.T, m *hcm.HttpConnectionManager) []byte {
	t.Helper()
	if len(m.AccessLog) != 1 {
		t.Fatalf("unexpected access log count %d", len(m.AccessLog))
	}
	anyLog := m.AccessLog[0].GetTypedConfig()
	if anyLog == nil {
		t.Fatal("access log has no typed config")
	}
	return anyLog.GetValue()
}

// TestCanonicalizeAnysIsOrderIndependent is the regression test for the
// filter_chain_is_being_removed bug: the same configuration must serialize to the same
// bytes no matter which order the Go maps happened to be iterated in. Without
// canonicalizeAnys the two payloads below differ, which makes Envoy think the listener
// changed and take the in-place filter chain update path.
func TestCanonicalizeAnysIsOrderIndependent(t *testing.T) {
	forward := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf",
		"hotel", "india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa"}
	backward := make([]string, 0, len(forward))
	for i := len(forward) - 1; i >= 0; i-- {
		backward = append(backward, forward[i])
	}

	a := buildHCMWithJSONAccessLog(t, forward)
	b := buildHCMWithJSONAccessLog(t, backward)

	canonicalizeAnys(a)
	canonicalizeAnys(b)

	pa, pb := accessLogPayload(t, a), accessLogPayload(t, b)
	if !bytes.Equal(pa, pb) {
		t.Fatalf("canonicalized payloads differ: %d vs %d bytes", len(pa), len(pb))
	}
	// And the canonical form must actually be key-sorted, otherwise the test above could
	// pass just because both conversions happened to pick the same random order.
	ia, iz := bytes.Index(pa, []byte("alpha")), bytes.Index(pa, []byte("papa"))
	if ia < 0 || iz < 0 || ia > iz {
		t.Fatalf("expected keys to be sorted in the canonical payload (alpha=%d papa=%d)", ia, iz)
	}
}

// TestCanonicalizeAnysIsIdempotent makes sure repeated canonicalization is stable, which
// matters because GetHTTPFiltersFromEnvoyFilter clones and re-processes the wrapper for
// every push.
func TestCanonicalizeAnysIsIdempotent(t *testing.T) {
	m := buildHCMWithJSONAccessLog(t, []string{"zulu", "yankee", "xray", "whiskey"})
	canonicalizeAnys(m)
	first := accessLogPayload(t, m)
	for i := 0; i < 20; i++ {
		canonicalizeAnys(m)
		if got := accessLogPayload(t, m); !bytes.Equal(first, got) {
			t.Fatalf("iteration %d changed the payload", i)
		}
	}
}

// TestCanonicalizeAnysKeepsTypeURL guards against rewriting the type URL: EnvoyFilter
// patches must reach Envoy with exactly the type that was configured.
func TestCanonicalizeAnysKeepsTypeURL(t *testing.T) {
	m := buildHCMWithJSONAccessLog(t, []string{"b", "a"})
	before := m.AccessLog[0].GetTypedConfig().GetTypeUrl()
	canonicalizeAnys(m)
	after := m.AccessLog[0].GetTypedConfig().GetTypeUrl()
	if before != after {
		t.Fatalf("type URL changed: %q -> %q", before, after)
	}
	if before == "" {
		t.Fatal("empty type URL")
	}
}

// TestCanonicalizeAnysNilAndEmpty makes sure the helper is safe on the inputs pilot feeds
// it in practice: nil messages and payloads it cannot resolve.
func TestCanonicalizeAnysNilAndEmpty(t *testing.T) {
	canonicalizeAnys(nil)

	m := &hcm.HttpConnectionManager{
		AccessLog: []*accesslog.AccessLog{{
			ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: &anypb.Any{TypeUrl: "type.googleapis.com/does.not.Exist"}},
		}},
	}
	canonicalizeAnys(m)
	if got := m.AccessLog[0].GetTypedConfig().GetTypeUrl(); got != "type.googleapis.com/does.not.Exist" {
		t.Fatalf("unresolvable payload was modified: %q", got)
	}
}
