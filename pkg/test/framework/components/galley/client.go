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
	"strings"
	"time"

	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/testing/monitoring"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type client struct {
	address string
}

func (c *client) waitForSnapshot(collection string, validator SnapshotValidatorFunc) error {
	conn, err := c.dialGrpc()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u := sink.NewInMemoryUpdater()

	cl := mcp.NewResourceSourceClient(conn)
	options := &sink.Options{
		CollectionOptions: sink.CollectionOptionsFromSlice([]string{collection}),
		Updater:           u,
		ID:                "",
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	mcpc := sink.NewClient(cl, options)
	go mcpc.Run(ctx)

	_, err = retry.Do(func() (interface{}, bool, error) {
		items := u.Get(collection)

		err = validator(toSnapshotObjects(items))
		if err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}, retry.Delay(time.Millisecond), retry.Timeout(time.Second*30))

	return err
}

func toSnapshotObjects(items []*sink.Object) []*SnapshotObject {
	snapshotObjects := make([]*SnapshotObject, 0, len(items))
	for _, item := range items {
		snapshotObjects = append(snapshotObjects, toSnapshotObject(item))
	}
	return snapshotObjects
}

func toSnapshotObject(item *sink.Object) *SnapshotObject {
	return &SnapshotObject{
		TypeURL:  item.TypeURL,
		Metadata: item.Metadata,
		Body:     item.Body,
	}
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
