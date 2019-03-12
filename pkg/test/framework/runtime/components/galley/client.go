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
	"strings"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	mcpclient "istio.io/istio/pkg/mcp/client"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/testing/monitoring"
	"istio.io/istio/pkg/test/framework/api/components"
	tcontext "istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type client struct {
	address string
	ctx     tcontext.Instance
}

func (c *client) waitForSnapshot(collection string, validator components.SnapshotValidator) error {
	conn, err := c.dialGrpc()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := sink.NewInMemoryUpdater()

	cl := mcp.NewAggregatedMeshConfigServiceClient(conn)
	options := &sink.Options{
		CollectionOptions: sink.CollectionOptionsFromSlice([]string{collection}),
		Updater:           u,
		ID:                "",
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	mcpc := mcpclient.New(cl, options)
	go mcpc.Run(ctx)

	var result *comparisonResult
	_, err = retry.Do(func() (interface{}, bool, error) {
		items := u.Get(collection)
		result, err = c.checkSnapshot(items, validator)
		if err != nil {
			return nil, false, err
		}
		return nil, result.err == nil, result.err
	}, retry.Delay(time.Millisecond), retry.Timeout(time.Second*30))

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

func (c *client) checkSnapshot(actual []*sink.Object, validator components.SnapshotValidator) (*comparisonResult, error) {
	// Convert the actuals to a map indexed by name.
	actualMap := make(map[string]*sink.Object)
	for _, a := range actual {
		actualMap[a.Metadata.Name] = a
	}

	var extraActual []string
	var missingExpected []string
	var conflicting []string
	var err error

	// If the given validator is a composite, compare the actual and expected maps.
	if compositeValidator, ok := validator.(components.CompositeSnapshotValidator); ok {
		// Check for missing expected objects.
		for name, a := range actualMap {
			if _, found := compositeValidator[name]; !found {
				extraActual = append(extraActual, name)

				// Create the error message.
				js, er := json.MarshalIndent(a, "", "  ")
				if er != nil {
					return nil, er
				}
				err = multierror.Append(err, fmt.Errorf("unexpected resource found: %s\n%v", name, string(js)))

				// Delete this key so we don't check for conflicts below.
				delete(actualMap, name)
				continue
			}
		}

		// Now check for missing actual objects.
		for name := range compositeValidator {
			if _, found := actualMap[name]; !found {
				missingExpected = append(missingExpected, name)
				err = multierror.Append(err, fmt.Errorf("expected resource not found: %s", name))

				// Note: We do not remove the key here, since we don't want to
				// modify the map that was passed in. In the conflict check below,
				// we iterate across the actualMap anyway, so we don't really care
				// about removing these elements.
				continue
			}
		}
	}

	// Now check for conflicts between actual and expected.
	for name, a := range actualMap {
		// Convert to SnapshotObject, which is a protobuf message.
		snapshotObj := &components.SnapshotObject{
			TypeURL:  a.TypeURL,
			Metadata: a.Metadata,
			Body:     a.Body,
		}
		if er := validator.ValidateObject(snapshotObj); er != nil {
			conflicting = append(conflicting, name)
			err = multierror.Append(err, er)
		}
	}

	return &comparisonResult{
		extraActual:     extraActual,
		missingExpected: missingExpected,
		conflicting:     conflicting,
		err:             err,
	}, nil
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

type comparisonResult struct {
	extraActual     []string
	missingExpected []string
	conflicting     []string
	err             error
}
