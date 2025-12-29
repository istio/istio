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

package security

import (
	"context"
	"time"

	retry "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/monitoring"
)

var caLog = log.RegisterScope("ca", "ca client")

// CARetryOptions returns the default retry options recommended for CA calls
// This includes 5 retries, with backoff from 100ms -> 1.6s with jitter.
var CARetryOptions = []retry.CallOption{
	retry.WithMax(5),
	retry.WithBackoff(wrapBackoffWithMetrics(retry.BackoffExponentialWithJitter(100*time.Millisecond, 0.1))),
	retry.WithCodes(codes.Canceled, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal, codes.Unavailable),
}

// CARetryInterceptor is a grpc UnaryInterceptor that adds retry options, as a convenience wrapper
// around CARetryOptions. If needed to chain with other interceptors, the CARetryOptions can be used
// directly.
func CARetryInterceptor() grpc.DialOption {
	return grpc.WithUnaryInterceptor(retry.UnaryClientInterceptor(CARetryOptions...))
}

// grpcretry has no hooks to trigger logic on failure (https://github.com/grpc-ecosystem/go-grpc-middleware/issues/375)
// Instead, we can wrap the backoff hook to log/increment metrics before returning the backoff result.
func wrapBackoffWithMetrics(bf retry.BackoffFunc) retry.BackoffFunc {
	return func(ctx context.Context, attempt uint) time.Duration {
		wait := bf(ctx, attempt)
		caLog.Warnf("ca request failed, starting attempt %d in %v", attempt, wait)
		monitoring.NumOutgoingRetries.With(monitoring.RequestType.Value(monitoring.CSR)).Increment()
		return wait
	}
}
