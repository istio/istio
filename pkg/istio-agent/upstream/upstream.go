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

package upstream

import (
	"context"
	"math"
	"sync"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/pkg/log"
)

const defaultClientMaxReceiveMessageSize = math.MaxInt32

type Upstream = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient

type Client struct {
	ctx    context.Context
	conn   *grpc.ClientConn
	logger *log.Scope
	// store the initial cds/nds request
	initialRequest []*discovery.DiscoveryRequest
}

func New(ctx context.Context, conn *grpc.ClientConn, logger *log.Scope) *Client {
	return &Client{
		ctx:    ctx,
		conn:   conn,
		logger: logger,
	}
}

func (c *Client) OpenStream(request <-chan *discovery.DiscoveryRequest) (<-chan *discovery.DiscoveryResponse, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	response := make(chan *discovery.DiscoveryResponse)

	go c.handleStreamsWithRetry(ctx, request, response)

	// We use context cancellation over using a separate channel for signalling stream shutdown.
	// The reason is cancelling a context tied with the stream is straightforward to signal closure.
	// Also, the shutdown function could potentially be called more than once by a caller.
	// Closing channels is not idempotent while cancelling context is idempotent.
	return response, func() { cancel() }
}

func (c *Client) handleStreamsWithRetry(
	ctx context.Context,
	requestCh <-chan *discovery.DiscoveryRequest,
	respCh chan<- *discovery.DiscoveryResponse) {

	for {
		childCtx, cancel := context.WithCancel(ctx)
		xds := discovery.NewAggregatedDiscoveryServiceClient(c.conn)
		stream, err := xds.StreamAggregatedResources(c.ctx,
			grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize))
		if err != nil {
			cancel()
			continue
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go c.send(childCtx, wg.Done, c.logger, cancel, stream, requestCh)
		go recv(childCtx, wg.Done, cancel, c.logger, respCh, stream)
		wg.Wait()
	}
}

// It is safe to assume send goroutine will not leak as long as these conditions are true:
// - SendMsg is performed with timeout.
// - send is a receiver for signal and exits when signal is closed by the owner.
// - send also exits on context cancellations.
func (c *Client) send(
	ctx context.Context,
	complete func(),
	logger *log.Scope,
	cancelFunc context.CancelFunc,
	stream Upstream,
	requestCh <-chan *discovery.DiscoveryRequest) {
	defer complete()

	// send the initial request
	for _, req := range c.initialRequest {
		// Ref: https://github.com/grpc/grpc-go/issues/1229#issuecomment-302755717
		// Call SendMsg in a timeout because it can block in some cases.
		err := DoWithTimeout(ctx, func() error {
			return stream.Send(req)
		}, 5*time.Second)
		if err != nil {
			handleError(ctx, logger, "Error in SendMsg", cancelFunc, err)
			return
		}
	}

	for {
		select {
		case request, ok := <-requestCh:
			if !ok {
				return
			}
			// this is the first cds request
			if request.TypeUrl == v3.ClusterType && len(c.initialRequest) == 0 {
				c.initialRequest = append(c.initialRequest, request)
			}

			// this is the first nds request, we only send once after lds
			if request.TypeUrl == v3.NameTableType {
				c.initialRequest = append(c.initialRequest, request)
			}

			// Ref: https://github.com/grpc/grpc-go/issues/1229#issuecomment-302755717
			// Call SendMsg in a timeout because it can block in some cases.
			err := DoWithTimeout(ctx, func() error {
				return stream.Send(request)
			}, 5*time.Second)
			if err != nil {
				handleError(ctx, logger, "Error in SendMsg", cancelFunc, err)
				return
			}
		case <-ctx.Done():
			_ = stream.CloseSend()
			return
		}
	}
}

// recv is an infinite loop which blocks on RecvMsg.
// The only ways to exit the goroutine is by cancelling the context or when an error occurs.
func recv(
	ctx context.Context,
	complete func(),
	cancelFunc context.CancelFunc,
	logger *log.Scope,
	responseCh chan<- *discovery.DiscoveryResponse,
	stream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient,
) {
	defer complete()
	for {
		resp, err := stream.Recv()
		if err != nil {
			handleError(ctx, logger, "Error in RecvMsg", cancelFunc, err)
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
			responseCh <- resp
		}
	}
}

// DoWithTimeout runs f and returns its error.
// If the timeout elapses first, returns a ctx timeout error instead.
func DoWithTimeout(ctx context.Context, f func() error, t time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	errChan := make(chan error, 1)
	go func() {
		errChan <- f()
		close(errChan)
	}()
	select {
	case <-timeoutCtx.Done():
		return timeoutCtx.Err()
	case err := <-errChan:
		return err
	}
}

func handleError(ctx context.Context, logger *log.Scope, errMsg string, cancelFunc context.CancelFunc, err error) {
	defer cancelFunc()
	select {
	case <-ctx.Done():
		// Context was cancelled, hence this is not an erroneous scenario.
		// Context is cancelled only when shutdown is called or any of the send/recv goroutines error out.
		// The shutdown can be called by the caller in many cases, during app shutdown/ttl expiry, etc
	default:
		logger.Errorf("%s: %s", errMsg, err.Error())
	}
}
