package echo

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang/sync/errgroup"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/test/application"
	"istio.io/istio/pkg/test/application/echo/proto"
)

// BatchOptions provides options to the batch processor.
type BatchOptions struct {
	Dialer  application.Dialer
	Count   int
	QPS     int
	Timeout time.Duration
	URL     string
	Header  http.Header
	Message string
	CAFile  string
}

// Batch processes a Batch of requests.
type Batch struct {
	options BatchOptions
	p       protocol
}

// Run the batch and collect the results.
func (b *Batch) Run() ([]string, error) {
	g, _ := errgroup.WithContext(context.Background())
	responses := make([]string, b.options.Count)

	var throttle <-chan time.Time

	if b.options.QPS > 0 {
		sleepTime := time.Second / time.Duration(b.options.QPS)
		log.Printf("Sleeping %v between requests\n", sleepTime)
		throttle = time.Tick(sleepTime)
	}

	for i := 0; i < b.options.Count; i++ {
		r := request{
			RequestID: i,
			URL:       b.options.URL,
			Message:   b.options.Message,
			Header:    b.options.Header,
		}
		r.RequestID = i

		if throttle != nil {
			<-throttle
		}

		respIndex := i
		g.Go(func() error {
			resp, err := b.p.makeRequest(&r)
			if err != nil {
				return err
			}
			responses[respIndex] = string(resp)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return responses, nil
}

// Close the batch processor.
func (b *Batch) Close() error {
	if b.p != nil {
		return b.p.Close()
	}
	return nil
}

// NewBatch creates a new batch processor with the given options.
func NewBatch(ops BatchOptions) (*Batch, error) {
	// Fill in the dialer with defaults.
	ops.Dialer = ops.Dialer.Fill()

	p, err := newProtocol(ops)
	if err != nil {
		return nil, err
	}

	b := &Batch{
		p:       p,
		options: ops,
	}

	return b, nil
}

func newProtocol(ops BatchOptions) (protocol, error) {
	if strings.HasPrefix(ops.URL, "http://") || strings.HasPrefix(ops.URL, "https://") {
		/* #nosec */
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			Timeout: ops.Timeout,
		}
		return &httpProtocol{
			client: client,
			do:     ops.Dialer.HTTP,
		}, nil
	} else if strings.HasPrefix(ops.URL, "grpc://") || strings.HasPrefix(ops.URL, "grpcs://") {
		secure := strings.HasPrefix(ops.URL, "grpcs://")
		var address string
		if secure {
			address = ops.URL[len("grpcs://"):]
		} else {
			address = ops.URL[len("grpc://"):]
		}

		// grpc-go sets incorrect authority header
		authority := ops.Header.Get(hostKey)

		// transport security
		security := grpc.WithInsecure()
		if secure {
			creds, err := credentials.NewClientTLSFromFile(ops.CAFile, authority)
			if err != nil {
				log.Fatalf("failed to load client certs %s %v", ops.CAFile, err)
			}
			security = grpc.WithTransportCredentials(creds)
		}

		ctx, cancel := context.WithTimeout(context.Background(), ops.Timeout)
		defer cancel()

		grpcConn, err := ops.Dialer.GRPC(ctx,
			address,
			security,
			grpc.WithAuthority(authority),
			grpc.WithBlock())
		if err != nil {
			return nil, err
		}
		client := proto.NewEchoTestServiceClient(grpcConn)
		return &grpcProtocol{
			conn:   grpcConn,
			client: client,
		}, nil
	} else if strings.HasPrefix(ops.URL, "ws://") || strings.HasPrefix(ops.URL, "wss://") {
		/* #nosec */
		dialer := &websocket.Dialer{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			HandshakeTimeout: ops.Timeout,
		}
		return &websocketProtocol{
			dialer: dialer,
		}, nil
	}

	return nil, fmt.Errorf("unrecognized protocol %q", ops.URL)
}
