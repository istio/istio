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
	tcontext "istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type client struct {
	address string
	ctx     tcontext.Instance
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

	var result *comparisonResult
	_, err = retry.Do(func() (interface{}, bool, error) {
		items := u.Get(typeURL)
		result, err = c.checkSnapshot(items, snapshot)
		if err != nil {
			return nil, false, err
		}
		err = result.generateError()
		return nil, err == nil, err
	}, retry.Delay(time.Millisecond), retry.Timeout(time.Second*5))

	if err != nil {
		// Create diffable folder structure
		dir, er := c.ctx.CreateTmpDirectory("snapshot")
		if er != nil {
			return multierror.Append(err, er)
		}
		actualPath, expectPath, er := result.generateDiffFolders(dir)
		if er != nil {
			return multierror.Append(err, er)
		}

		multierror.Append(err, fmt.Errorf("you can diff files here:\ndiff %s %s", expectPath, actualPath))
	}

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

func (c *client) checkSnapshot(actual []*mcpclient.Object, expected []map[string]interface{}) (*comparisonResult, error) {
	expectedMap := make(map[string]interface{})
	for _, e := range expected {
		name, err := extractName(e)
		if err != nil {
			return nil, err
		}
		expectedMap[name] = e
	}

	actualMap := make(map[string]interface{})
	for _, a := range actual {
		// Exclude ephemeral fields from comparison
		a.Metadata.CreateTime = nil
		a.Metadata.Version = ""

		b, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		o := make(map[string]interface{})
		if err = json.Unmarshal(b, &o); err != nil {
			return nil, err
		}

		name := a.Metadata.Name
		actualMap[name] = o
	}

	var extraActual []string
	var missingExpected []string
	var conflicting []string

	for name, a := range actualMap {
		e, found := expectedMap[name]
		if !found {
			extraActual = append(extraActual, name)
			continue
		}

		if !reflect.DeepEqual(a, e) {
			conflicting = append(conflicting, name)
		}
	}

	for name := range expectedMap {
		_, found := actualMap[name]
		if !found {
			missingExpected = append(missingExpected, name)
			continue
		}
	}

	return &comparisonResult{
		expected:        expectedMap,
		actual:          actualMap,
		extraActual:     extraActual,
		missingExpected: missingExpected,
		conflicting:     conflicting,
	}, nil
}

func extractName(i map[string]interface{}) (string, error) {
	m, found := i["Metadata"]
	if !found {
		return "", fmt.Errorf("metadata section not found in resource")
	}

	meta, ok := m.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("metadata section is not a map")
	}

	n, found := meta["name"]
	if !found {
		return "", fmt.Errorf("metadata section does not contain name")
	}

	name, ok := n.(string)
	if !ok {
		return "", fmt.Errorf("name field is not a string")
	}

	return name, nil
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
