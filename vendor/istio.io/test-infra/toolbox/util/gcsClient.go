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

package util

import (
	"bytes"
	"context"
	"fmt"

	"cloud.google.com/go/storage"
	"github.com/golang/glog"
	"google.golang.org/api/iterator"
)

// IGCSClient defines public functions of GCSClient
type IGCSClient interface {
	Exists(obj string) (bool, error)
	Read(obj string) (string, error)
	Write(obj, txt string) error
}

// GCSClient masks RPCs to gcs as local procedures
type GCSClient struct {
	client *storage.BucketHandle
	bucket string
}

// NewGCSClient creates a new GCSClient
func NewGCSClient(bucket string) *GCSClient {
	gcsClient, err := storage.NewClient(context.Background())
	if err != nil {
		glog.Fatalf("Failed to create a gcs client, %v", err)
		return nil
	}
	return &GCSClient{
		client: gcsClient.Bucket(bucket),
		bucket: bucket,
	}
}

// Exists finds if an object already exists on GCS bucket
func (gcs *GCSClient) Exists(obj string) (bool, error) {
	ctx := context.Background()
	query := &storage.Query{
		Prefix: obj,
	}
	iter := gcs.client.Objects(ctx, query)
	_, err := iter.Next()
	if err == iterator.Done {
		return false, nil
	} else if err != nil {
		glog.V(1).Infof("Failed to get a iterator on %s/%s from gcs, %v", gcs.bucket, obj, err)
		return false, err
	}
	return true, nil
}

// Read gets a file and return a string
func (gcs *GCSClient) Read(obj string) (string, error) {
	ctx := context.Background()

	r, err := gcs.client.Object(obj).NewReader(ctx)
	if err != nil {
		glog.V(1).Infof("Failed to open a reader on file %s/%s from gcs, %v", gcs.bucket, obj, err)
		return "", err
	}
	defer func() {
		if err = r.Close(); err != nil {
			glog.V(1).Infof("Failed to close gcs file reader, %v", err)
		}
	}()
	buf := new(bytes.Buffer)
	if _, err = buf.ReadFrom(r); err != nil {
		glog.V(1).Infof("Failed to read from gcs reader, %v", err)
		return "", err
	}
	return buf.String(), nil
}

// Write writes text to file on gcs
func (gcs *GCSClient) Write(obj, txt string) error {
	ctx := context.Background()
	w := gcs.client.Object(obj).NewWriter(ctx)
	if _, err := fmt.Fprint(w, txt); err != nil {
		glog.V(1).Infof("Failed to write to gcs: %v", err)
	}
	return w.Close()
}
