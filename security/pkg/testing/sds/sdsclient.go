// Package sds includes a lightweight testing client to interact with SDS.
package sds

import (
	"fmt"

	"istio.io/istio/pkg/adsc"
)

// Client is a lightweight client for testing secret discovery service server.
type Client struct {
	adsc *adsc.ADSC
}

// ClientOptions contains the options for the SDS testing
type ClientOptions struct {
	ServerAddress string
}

// NewClient returns a sds client for testing.
func NewClient(options ClientOptions) (*Client, error) {
	adsc, err := adsc.Dial(options.ServerAddress, "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create adsc client")
	}
	return &Client{
		adsc: adsc,
	}, nil
}
