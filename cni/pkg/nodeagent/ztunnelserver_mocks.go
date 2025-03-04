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

package nodeagent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"istio.io/istio/pkg/zdsapi"
)

type MockedZtunnelConnection struct {
	mock.Mock
}

func FakeZtunnelConnection() *MockedZtunnelConnection {
	return &MockedZtunnelConnection{}
}

func (m *MockedZtunnelConnection) Close() {
	m.Called()
}

func (m *MockedZtunnelConnection) UUID() uuid.UUID {
	args := m.Called()
	return args.Get(0).(uuid.UUID)
}

func (m *MockedZtunnelConnection) Updates() <-chan UpdateRequest {
	args := m.Called()
	return args.Get(0).(<-chan UpdateRequest)
}

func (m *MockedZtunnelConnection) CheckAlive(timeout time.Duration) error {
	args := m.Called(timeout)
	return args.Error(0)
}

func (m *MockedZtunnelConnection) ReadHello() (*zdsapi.ZdsHello, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*zdsapi.ZdsHello), args.Error(1)
}

func (m *MockedZtunnelConnection) Send(ctx context.Context, data *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error) {
	args := m.Called(ctx, data, fd)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*zdsapi.WorkloadResponse), args.Error(1)
}

func (m *MockedZtunnelConnection) SendMsgAndWaitForAck(msg *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error) {
	args := m.Called(msg, fd)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*zdsapi.WorkloadResponse), args.Error(1)
}
