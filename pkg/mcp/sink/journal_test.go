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

package sink

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/mcp/status"

	mcp "istio.io/api/mcp/v1alpha1"
)

func TestJournal(t *testing.T) {
	j := NewRequestJournal()

	wrap := 2
	var want []RecentRequestInfo
	for i := 0; i < journalDepth+wrap; i++ {
		req := &mcp.RequestResources{
			Collection:    "foo",
			ResponseNonce: fmt.Sprintf("nonce-%v", i),
		}

		j.RecordRequestResources(req)

		want = append(want, RecentRequestInfo{Request: req})
	}
	want = want[wrap:]

	ignoreTimeOption := cmp.Comparer(func(x, y time.Time) bool { return true })

	got := j.Snapshot()
	if diff := cmp.Diff(got, want, ignoreTimeOption); diff != "" {
		t.Fatalf("wrong Snapshot: \n got %v \nwant %v \ndiff %v", got, want, diff)
	}

	errorDetails, _ := status.FromError(errors.New("error"))
	req := &mcp.RequestResources{
		Collection:    "foo",
		ResponseNonce: "nonce-error",
		ErrorDetail:   errorDetails.Proto(),
	}
	j.RecordRequestResources(req)

	want = append(want, RecentRequestInfo{Request: req})
	want = want[1:]

	got = j.Snapshot()
	if diff := cmp.Diff(got, want, ignoreTimeOption); diff != "" {
		t.Fatalf("wrong Snapshot: \n got %v \nwant %v \ndiff %v", got, want, diff)
	}
	if got[len(got)-1].Acked() {
		t.Fatal("last request should be a NACK")
	}
}
