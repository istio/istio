// Copyright 2018 Istio Authors
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

package version

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"istio.io/pkg/log"
)

// Note that this code uses go.opencensus.io/stats,
// which depends on google.golang.org/grpc/internal/channelz,
// which is not available for OSX.  The build tag on line 1
// ensures this is only built on Linux.

var (
	gitTagKey       tag.Key
	componentTagKey tag.Key
	istioBuildTag   = stats.Int64(
		"istio/build",
		"Istio component build info",
		stats.UnitDimensionless)
)

// RecordComponentBuildTag sets the value for a metric that will be used to track component build tags for
// tracking rollouts, etc.
func (b BuildInfo) RecordComponentBuildTag(component string) {
	b.recordBuildTag(component, tag.New)
}

func (b BuildInfo) recordBuildTag(component string, newTagCtxFn func(context.Context, ...tag.Mutator) (context.Context, error)) {
	ctx := context.Background()
	var err error
	if ctx, err = newTagCtxFn(ctx, tag.Insert(gitTagKey, b.GitTag), tag.Insert(componentTagKey, component)); err != nil {
		log.Errorf("Could not establish build and component tag keys in context: %v", err)
	}
	stats.Record(ctx, istioBuildTag.M(1))
}

func init() {
	registerStats(tag.NewKey)
}

func registerStats(newTagKeyFn func(string) (tag.Key, error)) {
	var err error
	if gitTagKey, err = newTagKeyFn("tag"); err != nil {
		panic(err)
	}
	if componentTagKey, err = newTagKeyFn("component"); err != nil {
		panic(err)
	}
	gitTagView := &view.View{
		Measure:     istioBuildTag,
		TagKeys:     []tag.Key{componentTagKey, gitTagKey},
		Aggregation: view.LastValue(),
	}

	if err = view.Register(gitTagView); err != nil {
		panic(err)
	}
}
