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

// Package metadata contains all compiled-in adapter metadata
package metadata

import (
	"fmt"
	"time"

	"github.com/gogo/protobuf/types"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	bypass "istio.io/istio/mixer/adapter/bypass/config"
	circonus "istio.io/istio/mixer/adapter/circonus/config"
	cloudwatch "istio.io/istio/mixer/adapter/cloudwatch/config"
	denier "istio.io/istio/mixer/adapter/denier/config"
	dogstatsd "istio.io/istio/mixer/adapter/dogstatsd/config"
	fluentd "istio.io/istio/mixer/adapter/fluentd/config"
	kubernetesenv "istio.io/istio/mixer/adapter/kubernetesenv/config"
	list "istio.io/istio/mixer/adapter/list/config"
	memquota "istio.io/istio/mixer/adapter/memquota/config"
	opa "istio.io/istio/mixer/adapter/opa/config"
	prometheus "istio.io/istio/mixer/adapter/prometheus/config"
	redisquota "istio.io/istio/mixer/adapter/redisquota/config"
	solarwinds "istio.io/istio/mixer/adapter/solarwinds/config"
	stackdriver "istio.io/istio/mixer/adapter/stackdriver/config"
	statsd "istio.io/istio/mixer/adapter/statsd/config"
	stdio "istio.io/istio/mixer/adapter/stdio/config"
	zipkin "istio.io/istio/mixer/adapter/zipkin/config"

	ktmpl "istio.io/istio/mixer/adapter/kubernetesenv/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/authorization"
	"istio.io/istio/mixer/template/checknothing"
	edgepb "istio.io/istio/mixer/template/edge"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	"istio.io/istio/mixer/template/reportnothing"
	"istio.io/istio/mixer/template/tracespan"
)

var (
	Infos = []adapter.Info{
		{
			Name:        "bypass",
			Impl:        "istio.io/istio/mixer/adapter/bypass",
			Description: "Calls gRPC backends via the inline adapter model (useful for testing)",
			SupportedTemplates: []string{
				checknothing.TemplateName,
				reportnothing.TemplateName,
				metric.TemplateName,
				quota.TemplateName,
			},
			DefaultConfig: &bypass.Params{},
		},

		{
			Name:        "circonus",
			Description: "Emit metrics to Circonus.com monitoring endpoint",
			SupportedTemplates: []string{
				metric.TemplateName,
			},
			DefaultConfig: &circonus.Params{SubmissionUrl: "", SubmissionInterval: 10 * time.Second},
		},

		{
			Name:        "cloudwatch",
			Description: "Sends metrics to cloudwatch and logs to cloudwatchlogs",
			SupportedTemplates: []string{
				metric.TemplateName,
				logentry.TemplateName,
			},
			DefaultConfig: &cloudwatch.Params{},
		},

		{
			Name:        "denier",
			Impl:        "istio.io/istio/mixer/adapter/denier",
			Description: "Rejects any check and quota request with a configurable error",
			SupportedTemplates: []string{
				checknothing.TemplateName,
				listentry.TemplateName,
				quota.TemplateName,
			},
			DefaultConfig: &denier.Params{
				Status:        status.New(rpc.FAILED_PRECONDITION),
				ValidDuration: 5 * time.Second,
				ValidUseCount: 1000,
			},
		},

		{
			Name:        "dogstatsd",
			Description: "Produces dogstatsd metrics",
			SupportedTemplates: []string{
				metric.TemplateName,
			},
			DefaultConfig: &dogstatsd.Params{
				Address:      "localhost:8125",
				Prefix:       "istio.",
				BufferLength: 0,
				GlobalTags:   map[string]string{},
			},
		},

		{
			Name:        "fluentd",
			Description: "Sends logentrys to a fluentd instance",
			SupportedTemplates: []string{
				logentry.TemplateName,
			},
			DefaultConfig: &fluentd.Params{
				Address: "localhost:24224",
			},
		},

		{
			Name:        "kubernetesenv",
			Impl:        "istio.io/istio/mixer/adapter/kubernetesenv",
			Description: "Provides platform specific functionality for the kubernetes environment",
			SupportedTemplates: []string{
				ktmpl.TemplateName,
			},
			DefaultConfig: &kubernetesenv.Params{
				KubeconfigPath: "",
				// k8s cache invalidation
				// TODO: determine a reasonable default
				CacheRefreshDuration:       5 * time.Minute,
				ClusterRegistriesNamespace: "",
			},
		},

		{
			Name:               "listchecker",
			Impl:               "istio.io/istio/mixer/adapter/list",
			Description:        "Checks whether an entry is present in a list",
			SupportedTemplates: []string{listentry.TemplateName},
			DefaultConfig: &list.Params{
				RefreshInterval: 60 * time.Second,
				Ttl:             300 * time.Second,
				CachingInterval: 300 * time.Second,
				CachingUseCount: 10000,
				EntryType:       list.STRINGS,
				Blacklist:       false,
			},
		},

		{
			Name:        "memquota",
			Impl:        "istio.io/istio/mixer/adapter/memquota",
			Description: "Volatile memory-based quota tracking",
			SupportedTemplates: []string{
				quota.TemplateName,
			},
			DefaultConfig: &memquota.Params{
				MinDeduplicationDuration: 1 * time.Second,
			},
		},

		{
			Name:        "noop",
			Impl:        "istio.io/istio/mixer/adapter/noop",
			Description: "Does nothing (useful for testing)",
			SupportedTemplates: []string{
				authorization.TemplateName,
				checknothing.TemplateName,
				reportnothing.TemplateName,
				listentry.TemplateName,
				logentry.TemplateName,
				metric.TemplateName,
				quota.TemplateName,
				tracespan.TemplateName,
			},
			DefaultConfig: &types.Empty{},
		},

		{
			Name:        "opa",
			Impl:        "istio.io/istio/mixer/adapter/opa",
			Description: "Istio Authorization with Open Policy Agent engine",
			SupportedTemplates: []string{
				authorization.TemplateName,
			},
			DefaultConfig: &opa.Params{},
		},

		{
			Name:        "prometheus",
			Impl:        "istio.io/istio/mixer/adapter/prometheus",
			Description: "Publishes prometheus metrics",
			SupportedTemplates: []string{
				metric.TemplateName,
			},
			DefaultConfig: &prometheus.Params{},
		},

		{
			Name:        "redisquota",
			Impl:        "istio.io/mixer/adapter/redisquota",
			Description: "Redis-based quotas.",
			SupportedTemplates: []string{
				quota.TemplateName,
			},
			DefaultConfig: &redisquota.Params{
				RedisServerUrl:     "localhost:6379",
				ConnectionPoolSize: 10,
			},
		},

		{
			Name:        "solarwinds",
			Impl:        "istio.io/istio/mixer/adapter/solarwinds",
			Description: "Publishes metrics to appoptics and logs to papertrail",
			SupportedTemplates: []string{
				metric.TemplateName,
				logentry.TemplateName,
			},
			DefaultConfig: &solarwinds.Params{},
		},

		{
			Name:        "stackdriver",
			Impl:        "istio.io/istio/mixer/adapter/stackdriver",
			Description: "Publishes StackDriver metrics, logs and traces.",
			SupportedTemplates: []string{
				edgepb.TemplateName,
				logentry.TemplateName,
				metric.TemplateName,
				tracespan.TemplateName,
			},
			DefaultConfig: &stackdriver.Params{},
		},

		{
			Name:        "statsd",
			Impl:        "istio.io/istio/mixer/adapter/statsd",
			Description: "Produces statsd metrics",
			SupportedTemplates: []string{
				metric.TemplateName,
			},
			DefaultConfig: &statsd.Params{
				Address:       "localhost:8125",
				Prefix:        "",
				FlushDuration: 300 * time.Millisecond,
				FlushBytes:    512,
				SamplingRate:  1.0,
			},
		},

		{
			Name:        "stdio",
			Impl:        "istio.io/istio/mixer/adapter/stdio",
			Description: "Writes logs and metrics to a standard I/O stream",
			SupportedTemplates: []string{
				logentry.TemplateName,
				metric.TemplateName,
			},
			DefaultConfig: &stdio.Params{
				LogStream:                  stdio.STDOUT,
				MetricLevel:                stdio.INFO,
				OutputLevel:                stdio.INFO,
				OutputAsJson:               true,
				MaxDaysBeforeRotation:      30,
				MaxMegabytesBeforeRotation: 100 * 1024 * 1024,
				MaxRotatedFiles:            1000,
				SeverityLevels: map[string]stdio.Params_Level{
					"INFORMATIONAL": stdio.INFO,
					"informational": stdio.INFO,
					"INFO":          stdio.INFO,
					"info":          stdio.INFO,
					"WARNING":       stdio.WARNING,
					"warning":       stdio.WARNING,
					"WARN":          stdio.WARNING,
					"warn":          stdio.WARNING,
					"ERROR":         stdio.ERROR,
					"error":         stdio.ERROR,
					"ERR":           stdio.ERROR,
					"err":           stdio.ERROR,
					"FATAL":         stdio.ERROR,
					"fatal":         stdio.ERROR,
				},
			},
		},

		{
			Name:        "zipkin",
			Impl:        "istio.io/istio/mixer/adapter/zipkin",
			Description: "Publishes traces to Zipkin.",
			SupportedTemplates: []string{
				tracespan.TemplateName,
			},
			DefaultConfig: &zipkin.Params{},
		},
	}
)

// GetInfo looks up an adapter info from the declaration list by name
func GetInfo(name string) adapter.Info {
	for _, info := range Infos {
		if info.Name == name {
			return info
		}
	}
	panic(fmt.Errorf("requesting a missing descriptor %q", name))
}

// InfoMap returns an indexed map of adapter infos.
func InfoMap() map[string]*adapter.Info {
	out := make(map[string]*adapter.Info, len(Infos))
	for i := range Infos {
		info := Infos[i]
		out[info.Name] = &info
	}
	return out
}
