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

package grpc

import (
	"errors"
	"io"
	"testing"
)

func TestIsExpectedGRPCError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorType
	}{
		{
			name: "RST_STREAM",
			err:  errors.New("code = Internal desc = stream terminated by RST_STREAM with error code: NO_ERROR"),
			want: ExpectedError,
		},
		{
			name: "EOF",
			err:  io.EOF,
			want: GracefulTermination,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GRPCErrorType(tc.err); got != tc.want {
				t.Fatalf("expected got %v, but want: %v", got, tc.want)
			}
		})
	}
}
