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

package endpoint

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEchoForcesPlainTextContentType(t *testing.T) {
	query := url.Values{
		"headers": {"content-type:text/html,CoNtEnT-TyPe:application/xhtml+xml,X-CoNtEnT-TyPe-OpTiOnS:unsafe,X-Test-Header:retained"},
		"codes":   {"200"},
	}.Encode()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/?"+query, nil)
	response := httptest.NewRecorder()

	(&httpHandler{}).echo(response, request, uuid.New())

	if got, want := response.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Header().Get("X-Content-Type-Options"), "nosniff"; got != want {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, want)
	}
	if got, want := response.Header().Get("X-Test-Header"), "retained"; got != want {
		t.Fatalf("X-Test-Header = %q, want %q", got, want)
	}
	for key := range response.Header() {
		if strings.EqualFold(key, "Content-Type") && key != "Content-Type" {
			t.Fatalf("response retained non-canonical Content-Type header %q", key)
		}
		if strings.EqualFold(key, "X-Content-Type-Options") && key != "X-Content-Type-Options" {
			t.Fatalf("response retained non-canonical X-Content-Type-Options header %q", key)
		}
	}
}
