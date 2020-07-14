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

package keyval

import (
	"context"
	"fmt"
	"time"

	"github.com/gogo/protobuf/types"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	policy "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/status"
)

// Keyval implements the key-value template.
type Keyval struct{}

// HandleKeyval implements the gRPC stub
func (Keyval) HandleKeyval(ctx context.Context, req *HandleKeyvalRequest) (*HandleKeyvalResponse, error) {
	params := &Params{}
	if err := params.Unmarshal(req.AdapterConfig.Value); err != nil {
		return nil, err
	}
	key := req.Instance.Key
	fmt.Printf("look up %q\n", key)
	value, ok := params.Table[key]
	if ok {
		return &HandleKeyvalResponse{
			Result: &v1beta1.CheckResult{ValidDuration: 5 * time.Second},
			Output: &OutputMsg{Value: value},
		}, nil
	}
	return &HandleKeyvalResponse{
		Result: &v1beta1.CheckResult{
			Status: rpc.Status{
				Code: int32(rpc.NOT_FOUND),
				Details: []*types.Any{status.PackErrorDetail(&policy.DirectHttpResponse{
					Body: fmt.Sprintf("<error_detail>key %q not found</error_detail>", key),
				})},
			},
		},
	}, nil
}
