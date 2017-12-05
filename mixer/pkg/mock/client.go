// Copyright 2017 Istio Authors
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

package mock

import (
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
)

// NewClient returns a Mixer Grpc client.
func NewClient(mixerAddr string) (mixerpb.MixerClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(mixerAddr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return mixerpb.NewMixerClient(conn), conn, nil
}
