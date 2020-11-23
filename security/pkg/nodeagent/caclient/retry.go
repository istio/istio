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

package caclient

import (
	"time"

	retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"google.golang.org/grpc/codes"
)

// RetryOptions returns the default retry options recommended for CA calls
// This includes 5 retries, with backoff from 100ms -> 1.6s with jitter.
var RetryOptions = []retry.CallOption{
	retry.WithMax(5),
	retry.WithBackoff(retry.BackoffExponentialWithJitter(100*time.Millisecond, 0.1)),
	retry.WithCodes(codes.Canceled, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal, codes.Unavailable),
}
