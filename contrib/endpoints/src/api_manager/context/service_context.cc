// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/context/service_context.h"

#include "src/api_manager/service_control/aggregated.h"

namespace google {
namespace api_manager {
namespace context {

namespace {

// Default Cloud Trace URL. Points to prod Cloud Trace.
const char kCloudTraceUrl[] = "https://cloudtrace.googleapis.com";

// Default maximum time to aggregate traces.
const int kDefaultAggregateTimeMillisec = 1000;

// Default maximum amount of traces to aggregate. The amount should ensure
// the http request payload with the aggregated traces not reaching MB in size.
const int kDefaultTraceCacheMaxSize = 100;

// Default trace sample rate, in QPS.
const double kDefaultTraceSampleQps = 0.1;

// The time window to send intermediate report for Grpc streaming (second).
// Default to 10s.
const int kIntermediateReportInterval = 10;
}

ServiceContext::ServiceContext(std::unique_ptr<ApiManagerEnvInterface> env,
                               std::unique_ptr<Config> config)
    : env_(std::move(env)),
      config_(std::move(config)),
      service_account_token_(env_.get()),
      service_control_(CreateInterface()),
      cloud_trace_aggregator_(CreateCloudTraceAggregator()),
      is_auth_force_disabled_(config_->server_config() &&
                              config_->server_config()
                                  ->api_authentication_config()
                                  .force_disable()) {
  intermediate_report_interval_ = kIntermediateReportInterval;

  // Check server_config override.
  if (config_->server_config() &&
      config_->server_config()->has_service_control_config() &&
      config_->server_config()
          ->service_control_config()
          .intermediate_report_min_interval()) {
    intermediate_report_interval_ = config_->server_config()
                                        ->service_control_config()
                                        .intermediate_report_min_interval();
  }
}

MethodCallInfo ServiceContext::GetMethodCallInfo(
    const std::string& http_method, const std::string& url,
    const std::string& query_params) const {
  if (config_ == nullptr) {
    return MethodCallInfo();
  }
  return config_->GetMethodCallInfo(http_method, url, query_params);
}

const std::string& ServiceContext::project_id() const {
  if (gce_metadata_.has_valid_data() && !gce_metadata_.project_id().empty()) {
    return gce_metadata_.project_id();
  } else {
    return config_->service().producer_project_id();
  }
}

std::unique_ptr<service_control::Interface> ServiceContext::CreateInterface() {
  return std::unique_ptr<service_control::Interface>(
      service_control::Aggregated::Create(config_->service(),
                                          config_->server_config(), env_.get(),
                                          &service_account_token_));
}

std::unique_ptr<cloud_trace::Aggregator>
ServiceContext::CreateCloudTraceAggregator() {
  // If force_disable is set in server config, completely disable tracing.
  if (config_->server_config() &&
      config_->server_config()->cloud_tracing_config().force_disable()) {
    env()->LogInfo(
        "Cloud Trace is force disabled. There will be no trace written.");
    return std::unique_ptr<cloud_trace::Aggregator>();
  }

  std::string url = kCloudTraceUrl;
  int aggregate_time_millisec = kDefaultAggregateTimeMillisec;
  int cache_max_size = kDefaultTraceCacheMaxSize;
  double minimum_qps = kDefaultTraceSampleQps;
  if (config_->server_config() &&
      config_->server_config()->has_cloud_tracing_config()) {
    // If url_override is set in server config, use it to query Cloud Trace.
    const auto& tracing_config =
        config_->server_config()->cloud_tracing_config();
    if (!tracing_config.url_override().empty()) {
      url = tracing_config.url_override();
    }

    // If aggregation config is set, take the values from it.
    if (tracing_config.has_aggregation_config()) {
      aggregate_time_millisec =
          tracing_config.aggregation_config().time_millisec();
      cache_max_size = tracing_config.aggregation_config().cache_max_size();
    }

    // If sampling config is set, take the values from it.
    if (tracing_config.has_samling_config()) {
      minimum_qps = tracing_config.samling_config().minimum_qps();
    }
  }

  return std::unique_ptr<cloud_trace::Aggregator>(new cloud_trace::Aggregator(
      &service_account_token_, url, aggregate_time_millisec, cache_max_size,
      minimum_qps, env_.get()));
}

}  // namespace context
}  // namespace api_manager
}  // namespace google
