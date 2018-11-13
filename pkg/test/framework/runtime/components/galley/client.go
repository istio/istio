//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package galley

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"
	mcp "istio.io/api/mcp/v1alpha1"
	mcpclient "istio.io/istio/pkg/mcp/client"
	"istio.io/istio/pkg/mcp/testing/monitoring"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type client struct {
	env     api.Environment
	address string
}

func (c *client) waitForSnapshot(typeURL string, snapshot []map[string]interface{}) error {
	conn, err := c.dialGrpc()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	urls := []string{typeURL}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := mcpclient.NewInMemoryUpdater()

	cl := mcp.NewAggregatedMeshConfigServiceClient(conn)
	mcpc := mcpclient.New(cl, urls, u, "", map[string]string{}, mcptestmon.NewInMemoryClientStatsContext())
	go mcpc.Run(ctx)

	_, err = retry.Do(func() (interface{}, bool, error) {
		items := u.Get(typeURL)
		err := checkSnapshot(items, snapshot)
		return nil, err == nil, err
	}, retry.Delay(time.Millisecond), retry.Timeout(time.Second*10))

	return err
}

func (c *client) waitForStartup() (err error) {
	_, err = retry.Do(func() (interface{}, bool, error) {
		conn, err2 := c.dialGrpc()
		if err != nil {
			return nil, false, err2
		}
		_ = conn.Close()
		return nil, true, nil
	})

	return
}

func checkSnapshot(actual []*mcpclient.Object, expected []map[string]interface{}) (err error) {
	var actualArr []interface{}
	for _, a := range actual {

		// Exclude ephemeral fields from comparison
		a.Metadata.CreateTime = nil
		a.Metadata.Version = ""

		b, err := json.Marshal(a)
		if err != nil {
			return err
		}
		o := make(map[string]interface{})
		if err = json.Unmarshal(b, &o); err != nil {
			return err
		}
		actualArr = append(actualArr, o)
	}

	expectedArr := make([]map[string]interface{}, len(expected), len(expected))
	copy(expectedArr, expected)

mainloop:
	for i, a := range actualArr {
		for j, e := range expectedArr {
			if e == nil {
				// This was visited before.
				continue
			}

			if reflect.DeepEqual(a, e) {
				expectedArr[j] = nil
				actualArr[i] = nil
				continue mainloop
			}
		}
	}

	// If any of the actual or expected left, build errors for them.
	for _, e := range expectedArr {
		if e == nil {
			continue
		}

		js, er := json.MarshalIndent(e, "", "  ")
		if er != nil {
			return er
		}
		err = multierror.Append(err, fmt.Errorf("expected resource not found: %v", string(js)))
	}

	for _, a := range actualArr {
		if a == nil {
			continue
		}

		js, er := json.MarshalIndent(a, "", "  ")
		if er != nil {
			return er
		}
		err = multierror.Append(err, fmt.Errorf("unexpected resource: %v", string(js)))
	}

	return err
}

func (c *client) dialGrpc() (*grpc.ClientConn, error) {

	addr := c.address
	if strings.HasPrefix(c.address, "tcp://") {
		addr = c.address[6:]
	}

	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {

		return nil, err
	}
	scopes.Framework.Debug("connected to Galley pod through port forwarder")
	return conn, nil
}

// Close implements io.Closer.
func (c *client) Close() (err error) {

	return err
}
