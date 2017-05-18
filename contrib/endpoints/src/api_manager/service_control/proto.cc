// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/api_manager/service_control/proto.h"

#include <functional>

#include <time.h>
#include <chrono>

#include "contrib/endpoints/include/api_manager/service_control.h"
#include "contrib/endpoints/include/api_manager/utils/version.h"
#include "contrib/endpoints/src/api_manager/auth/lib/auth_token.h"
#include "contrib/endpoints/src/api_manager/auth/lib/base64.h"
#include "google/api/metric.pb.h"
#include "google/protobuf/timestamp.pb.h"
#include "utils/distribution_helper.h"

using ::google::api::servicecontrol::v1::CheckError;
using ::google::api::servicecontrol::v1::QuotaError;
using ::google::api::servicecontrol::v1::CheckRequest;
using ::google::api::servicecontrol::v1::CheckResponse;
using ::google::api::servicecontrol::v1::Distribution;
using ::google::api::servicecontrol::v1::LogEntry;
using ::google::api::servicecontrol::v1::MetricValue;
using ::google::api::servicecontrol::v1::MetricValueSet;
using ::google::api::servicecontrol::v1::Operation;
using ::google::api::servicecontrol::v1::ReportRequest;
using ::google::api_manager::utils::Status;
using ::google::protobuf::Map;
using ::google::protobuf::StringPiece;
using ::google::protobuf::Timestamp;
using ::google::protobuf::util::error::Code;
using ::google::service_control_client::DistributionHelper;

namespace google {
namespace api_manager {
namespace service_control {

const char kConsumerQuotaUsedCount[] =
    "serviceruntime.googleapis.com/api/consumer/quota_used_count";

const char kQuotaName[] = "/quota_name";

struct SupportedMetric {
  const char* name;
  ::google::api::MetricDescriptor_MetricKind metric_kind;
  ::google::api::MetricDescriptor_ValueType value_type;

  enum Mark { PRODUCER = 0, CONSUMER = 1 };
  enum Tag { START = 0, INTERMEDIATE = 1, FINAL = 2 };
  Tag tag;
  Mark mark;
  Status (*set)(const SupportedMetric& m, const ReportRequestInfo& info,
                Operation* operation);
};

struct SupportedLabel {
  const char* name;
  ::google::api::LabelDescriptor_ValueType value_type;

  enum Kind { USER = 0, SYSTEM = 1 };
  Kind kind;

  Status (*set)(const SupportedLabel& l, const ReportRequestInfo& info,
                Map<std::string, std::string>* labels);
};

namespace {

// Metric Helpers

MetricValue* AddMetricValue(const char* metric_name, Operation* operation) {
  MetricValueSet* metric_value_set = operation->add_metric_value_sets();
  metric_value_set->set_metric_name(metric_name);
  return metric_value_set->add_metric_values();
}

void AddInt64Metric(const char* metric_name, int64_t value,
                    Operation* operation) {
  MetricValue* metric_value = AddMetricValue(metric_name, operation);
  metric_value->set_int64_value(value);
}

// The parameters to initialize DistributionHelper
struct DistributionHelperOptions {
  int buckets;
  double growth;
  double scale;
};

const DistributionHelperOptions time_distribution = {29, 2.0, 1e-6};
const DistributionHelperOptions size_distribution = {8, 10.0, 1};
const double kMsToSecs = 1e-3;

Status AddDistributionMetric(const DistributionHelperOptions& options,
                             const char* metric_name, double value,
                             Operation* operation) {
  MetricValue* metric_value = AddMetricValue(metric_name, operation);
  Distribution distribution;
  ::google::protobuf::util::Status proto_status =
      DistributionHelper::InitExponential(options.buckets, options.growth,
                                          options.scale, &distribution);
  if (!proto_status.ok()) return Status::FromProto(proto_status);
  proto_status = DistributionHelper::AddSample(value, &distribution);
  if (!proto_status.ok()) return Status::FromProto(proto_status);
  *metric_value->mutable_distribution_value() = distribution;
  return Status::OK;
}

// Metrics supported by ESP.

Status set_int64_metric_to_constant_1(const SupportedMetric& m,
                                      const ReportRequestInfo& info,
                                      Operation* operation) {
  AddInt64Metric(m.name, 1l, operation);
  return Status::OK;
}

Status set_int64_metric_to_constant_1_if_http_error(
    const SupportedMetric& m, const ReportRequestInfo& info,
    Operation* operation) {
  // Use status code >= 400 to determine request failed.
  if (info.response_code >= 400) {
    AddInt64Metric(m.name, 1l, operation);
  }
  return Status::OK;
}

Status set_distribution_metric_to_request_size(const SupportedMetric& m,
                                               const ReportRequestInfo& info,
                                               Operation* operation) {
  if (info.request_size >= 0) {
    return AddDistributionMetric(size_distribution, m.name, info.request_size,
                                 operation);
  }
  return Status::OK;
}

Status set_distribution_metric_to_response_size(const SupportedMetric& m,
                                                const ReportRequestInfo& info,
                                                Operation* operation) {
  if (info.response_size >= 0) {
    return AddDistributionMetric(size_distribution, m.name, info.response_size,
                                 operation);
  }
  return Status::OK;
}

// TODO: Consider refactoring following 3 functions to avoid duplicate code
Status set_distribution_metric_to_request_time(const SupportedMetric& m,
                                               const ReportRequestInfo& info,
                                               Operation* operation) {
  if (info.latency.request_time_ms >= 0) {
    double request_time_secs = info.latency.request_time_ms * kMsToSecs;
    return AddDistributionMetric(time_distribution, m.name, request_time_secs,
                                 operation);
  }
  return Status::OK;
}

Status set_distribution_metric_to_backend_time(const SupportedMetric& m,
                                               const ReportRequestInfo& info,
                                               Operation* operation) {
  if (info.latency.backend_time_ms >= 0) {
    double backend_time_secs = info.latency.backend_time_ms * kMsToSecs;
    return AddDistributionMetric(time_distribution, m.name, backend_time_secs,
                                 operation);
  }
  return Status::OK;
}

Status set_distribution_metric_to_overhead_time(const SupportedMetric& m,
                                                const ReportRequestInfo& info,
                                                Operation* operation) {
  if (info.latency.overhead_time_ms >= 0) {
    double overhead_time_secs = info.latency.overhead_time_ms * kMsToSecs;
    return AddDistributionMetric(time_distribution, m.name, overhead_time_secs,
                                 operation);
  }
  return Status::OK;
}

Status set_int64_metric_to_request_bytes(const SupportedMetric& m,
                                         const ReportRequestInfo& info,
                                         Operation* operation) {
  if (info.request_bytes > 0) {
    AddInt64Metric(m.name, info.request_bytes, operation);
  }
  return Status::OK;
}

Status set_int64_metric_to_response_bytes(const SupportedMetric& m,
                                          const ReportRequestInfo& info,
                                          Operation* operation) {
  if (info.response_bytes > 0) {
    AddInt64Metric(m.name, info.response_bytes, operation);
  }
  return Status::OK;
}

Status set_distribution_metric_to_streaming_request_message_counts(
    const SupportedMetric& m, const ReportRequestInfo& info,
    Operation* operation) {
  if (info.streaming_request_message_counts > 0) {
    AddDistributionMetric(size_distribution, m.name,
                          info.streaming_request_message_counts, operation);
  }
  return Status::OK;
}
Status set_distribution_metric_to_streaming_response_message_counts(
    const SupportedMetric& m, const ReportRequestInfo& info,
    Operation* operation) {
  if (info.streaming_response_message_counts > 0) {
    AddDistributionMetric(size_distribution, m.name,
                          info.streaming_response_message_counts, operation);
  }
  return Status::OK;
}

Status set_distribution_metric_to_streaming_durations(
    const SupportedMetric& m, const ReportRequestInfo& info,
    Operation* operation) {
  if (info.streaming_durations > 0) {
    AddDistributionMetric(time_distribution, m.name, info.streaming_durations,
                          operation);
  }
  return Status::OK;
}
// Currently unsupported metrics:
//
//  "serviceruntime.googleapis.com/api/producer/by_consumer/quota_used_count"
//
const SupportedMetric supported_metrics[] = {
    {
        "serviceruntime.googleapis.com/api/consumer/request_count",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64, SupportedMetric::START,
        SupportedMetric::CONSUMER, set_int64_metric_to_constant_1,
    },
    {
        "serviceruntime.googleapis.com/api/producer/request_count",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64, SupportedMetric::START,
        SupportedMetric::PRODUCER, set_int64_metric_to_constant_1,
    },
    {
        "serviceruntime.googleapis.com/api/producer/by_consumer/request_count",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64, SupportedMetric::FINAL,
        SupportedMetric::PRODUCER, set_int64_metric_to_constant_1,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/request_sizes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::CONSUMER,
        set_distribution_metric_to_request_size,
    },
    {
        "serviceruntime.googleapis.com/api/producer/request_sizes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_request_size,
    },
    {
        "serviceruntime.googleapis.com/api/producer/by_consumer/request_sizes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_request_size,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/response_sizes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::CONSUMER,
        set_distribution_metric_to_response_size,
    },
    {
        "serviceruntime.googleapis.com/api/producer/response_sizes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_response_size,
    },
    {
        "serviceruntime.googleapis.com/api/producer/by_consumer/response_sizes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_response_size,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/request_bytes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64,
        SupportedMetric::INTERMEDIATE, SupportedMetric::CONSUMER,
        set_int64_metric_to_request_bytes,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/response_bytes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64,
        SupportedMetric::INTERMEDIATE, SupportedMetric::CONSUMER,
        set_int64_metric_to_response_bytes,
    },
    {
        "serviceruntime.googleapis.com/api/producer/request_bytes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64,
        SupportedMetric::INTERMEDIATE, SupportedMetric::PRODUCER,
        set_int64_metric_to_request_bytes,
    },
    {
        "serviceruntime.googleapis.com/api/producer/response_bytes",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64,
        SupportedMetric::INTERMEDIATE, SupportedMetric::PRODUCER,
        set_int64_metric_to_response_bytes,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/error_count",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64, SupportedMetric::FINAL,
        SupportedMetric::CONSUMER, set_int64_metric_to_constant_1_if_http_error,
    },
    {
        "serviceruntime.googleapis.com/api/producer/error_count",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64, SupportedMetric::FINAL,
        SupportedMetric::PRODUCER, set_int64_metric_to_constant_1_if_http_error,
    },
    {
        "serviceruntime.googleapis.com/api/producer/by_consumer/error_count",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_INT64, SupportedMetric::FINAL,
        SupportedMetric::PRODUCER, set_int64_metric_to_constant_1_if_http_error,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/total_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::CONSUMER,
        set_distribution_metric_to_request_time,
    },
    {
        "serviceruntime.googleapis.com/api/producer/total_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_request_time,
    },
    {
        "serviceruntime.googleapis.com/api/producer/by_consumer/"
        "total_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_request_time,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/backend_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::CONSUMER,
        set_distribution_metric_to_backend_time,
    },
    {
        "serviceruntime.googleapis.com/api/producer/backend_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_backend_time,
    },
    {
        "serviceruntime.googleapis.com/api/producer/by_consumer/"
        "backend_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_backend_time,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/request_overhead_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::CONSUMER,
        set_distribution_metric_to_overhead_time,
    },
    {
        "serviceruntime.googleapis.com/api/producer/request_overhead_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_overhead_time,
    },
    {
        "serviceruntime.googleapis.com/api/producer/by_consumer/"
        "request_overhead_latencies",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_overhead_time,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/"
        "streaming_request_message_counts",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::CONSUMER,
        set_distribution_metric_to_streaming_request_message_counts,
    },
    {
        "serviceruntime.googleapis.com/api/producer/"
        "streaming_request_message_counts",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_streaming_request_message_counts,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/"
        "streaming_response_message_counts",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::CONSUMER,
        set_distribution_metric_to_streaming_response_message_counts,
    },
    {
        "serviceruntime.googleapis.com/api/producer/"
        "streaming_response_message_counts",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_streaming_response_message_counts,
    },
    {
        "serviceruntime.googleapis.com/api/consumer/"
        "streaming_durations",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::CONSUMER,
        set_distribution_metric_to_streaming_durations,
    },
    {
        "serviceruntime.googleapis.com/api/producer/"
        "streaming_durations",
        ::google::api::MetricDescriptor_MetricKind_DELTA,
        ::google::api::MetricDescriptor_ValueType_DISTRIBUTION,
        SupportedMetric::FINAL, SupportedMetric::PRODUCER,
        set_distribution_metric_to_streaming_durations,
    },

};
const int supported_metrics_count =
    sizeof(supported_metrics) / sizeof(supported_metrics[0]);

const char kServiceControlCallerIp[] =
    "servicecontrol.googleapis.com/caller_ip";
const char kServiceControlReferer[] = "servicecontrol.googleapis.com/referer";
const char kServiceControlServiceAgent[] =
    "servicecontrol.googleapis.com/service_agent";
const char kServiceControlUserAgent[] =
    "servicecontrol.googleapis.com/user_agent";
const char kServiceControlPlatform[] = "servicecontrol.googleapis.com/platform";
const char kServiceControlAndroidPackageName[] =
    "servicecontrol.googleapis.com/android_package_name";
const char kServiceControlAndroidCertFingerprint[] =
    "servicecontrol.googleapis.com/android_cert_fingerprint";
const char kServiceControlIosBundleId[] =
    "servicecontrol.googleapis.com/ios_bundle_id";
const char kServiceControlBackendProtocol[] =
    "servicecontrol.googleapis.com/backend_protocol";

// User agent label value
// The value for kUserAgent should be configured at service control server.
// Now it is configured as "ESP".
const char kUserAgent[] = "ESP";

// Service agent label value
const char kServiceAgentPrefix[] = "ESP/";

// /credential_id
Status set_credential_id(const SupportedLabel& l, const ReportRequestInfo& info,
                         Map<std::string, std::string>* labels) {
  // The rule to set /credential_id is:
  // 1) If api_key is available, set it as apiKey:API-KEY
  // 2) If auth issuer and audience both are available, set it as:
  //    jwtAuth:issuer=base64(issuer)&audience=base64(audience)
  if (!info.api_key.empty()) {
    std::string credential_id("apikey:");
    credential_id += info.api_key.ToString();
    (*labels)[l.name] = credential_id;
  } else if (!info.auth_issuer.empty()) {
    // If auth is used, auth_issuer should NOT be empty since it is required.
    char* base64_issuer = auth::esp_base64_encode(
        info.auth_issuer.data(), info.auth_issuer.size(), true /* url_safe */,
        false /* multiline */, false /* padding */);
    if (base64_issuer == nullptr) {
      return Status(Code::INTERNAL, "Out of memory");
    }
    std::string credential_id("jwtauth:issuer=");
    credential_id += base64_issuer;
    auth::esp_grpc_free(base64_issuer);

    // auth audience is optional.
    if (!info.auth_audience.empty()) {
      char* base64_audience = auth::esp_base64_encode(
          info.auth_audience.data(), info.auth_audience.size(),
          true /* url_safe */, false /* multiline */, false /* padding */);
      if (base64_audience == nullptr) {
        return Status(Code::INTERNAL, "Out of memory");
      }

      credential_id += "&audience=";
      credential_id += base64_audience;
      auth::esp_grpc_free(base64_audience);
    }
    (*labels)[l.name] = credential_id;
  }
  return Status::OK;
}

const char* error_types[10] = {"0xx", "1xx", "2xx", "3xx", "4xx",
                               "5xx", "6xx", "7xx", "8xx", "9xx"};

// /error_type
Status set_error_type(const SupportedLabel& l, const ReportRequestInfo& info,
                      Map<std::string, std::string>* labels) {
  if (info.response_code >= 400) {
    int code = (info.response_code / 100) % 10;
    if (error_types[code]) {
      (*labels)[l.name] = error_types[code];
    }
  }
  return Status::OK;
}

// /protocol
Status set_protocol(const SupportedLabel& l, const ReportRequestInfo& info,
                    Map<std::string, std::string>* labels) {
  (*labels)[l.name] = protocol::ToString(info.frontend_protocol);
  return Status::OK;
}

// /servicecontrol.googleapis.com/backend_protocol
Status set_backend_protocol(const SupportedLabel& l,
                            const ReportRequestInfo& info,
                            Map<std::string, std::string>* labels) {
  // backend_protocol is either GRPC or UNKNOWN.
  if (info.backend_protocol == protocol::GRPC &&
      info.frontend_protocol != info.backend_protocol) {
    (*labels)[l.name] = protocol::ToString(info.backend_protocol);
  }
  return Status::OK;
}

// /referer
Status set_referer(const SupportedLabel& l, const ReportRequestInfo& info,
                   Map<std::string, std::string>* labels) {
  if (!info.referer.empty()) {
    (*labels)[l.name] = info.referer;
  }
  return Status::OK;
}

// /response_code
Status set_response_code(const SupportedLabel& l, const ReportRequestInfo& info,
                         Map<std::string, std::string>* labels) {
  if (!info.is_final_report) return Status::OK;
  char response_code_buf[20];
  snprintf(response_code_buf, sizeof(response_code_buf), "%d",
           info.response_code);
  (*labels)[l.name] = response_code_buf;
  return Status::OK;
}

// /response_code_class
Status set_response_code_class(const SupportedLabel& l,
                               const ReportRequestInfo& info,
                               Map<std::string, std::string>* labels) {
  if (!info.is_final_report) return Status::OK;
  (*labels)[l.name] = error_types[(info.response_code / 100) % 10];
  return Status::OK;
}

// /status_code
Status set_status_code(const SupportedLabel& l, const ReportRequestInfo& info,
                       Map<std::string, std::string>* labels) {
  if (!info.is_final_report) return Status::OK;
  char status_code_buf[20];
  snprintf(status_code_buf, sizeof(status_code_buf), "%d",
           info.status.CanonicalCode());
  (*labels)[l.name] = status_code_buf;
  return Status::OK;
}

// cloud.googleapis.com/location
Status set_location(const SupportedLabel& l, const ReportRequestInfo& info,
                    Map<std::string, std::string>* labels) {
  if (!info.location.empty()) {
    (*labels)[l.name] = info.location;
  }
  return Status::OK;
}

// serviceruntime.googleapis.com/api_method
Status set_api_method(const SupportedLabel& l, const ReportRequestInfo& info,
                      Map<std::string, std::string>* labels) {
  if (!info.api_method.empty()) {
    (*labels)[l.name] = info.api_method;
  }
  return Status::OK;
}

// serviceruntime.googleapis.com/api_version
Status set_api_version(const SupportedLabel& l, const ReportRequestInfo& info,
                       Map<std::string, std::string>* labels) {
  if (!info.api_version.empty()) {
    (*labels)[l.name] = info.api_version;
  }
  return Status::OK;
}

// servicecontrol.googleapis.com/platform
Status set_platform(const SupportedLabel& l, const ReportRequestInfo& info,
                    Map<std::string, std::string>* labels) {
  (*labels)[l.name] = compute_platform::ToString(info.compute_platform);
  return Status::OK;
}

// servicecontrol.googleapis.com/service_agent
Status set_service_agent(const SupportedLabel& l, const ReportRequestInfo& info,
                         Map<std::string, std::string>* labels) {
  (*labels)[l.name] = kServiceAgentPrefix + utils::Version::instance().get();
  return Status::OK;
}

// serviceruntime.googleapis.com/user_agent
Status set_user_agent(const SupportedLabel& l, const ReportRequestInfo& info,
                      Map<std::string, std::string>* labels) {
  (*labels)[l.name] = kUserAgent;
  return Status::OK;
}

const SupportedLabel supported_labels[] = {
    {
        "/credential_id", ::google::api::LabelDescriptor_ValueType_STRING,
        SupportedLabel::USER, set_credential_id,
    },
    {
        "/end_user", ::google::api::LabelDescriptor_ValueType_STRING,
        SupportedLabel::USER, nullptr,
    },
    {
        "/end_user_country", ::google::api::LabelDescriptor_ValueType_STRING,
        SupportedLabel::USER, nullptr,
    },
    {
        "/error_type", ::google::api::LabelDescriptor_ValueType_STRING,
        SupportedLabel::USER, set_error_type,
    },
    {
        "/protocol", ::google::api::LabelDescriptor::STRING,
        SupportedLabel::USER, set_protocol,
    },
    {
        "/referer", ::google::api::LabelDescriptor_ValueType_STRING,
        SupportedLabel::USER, set_referer,
    },
    {
        "/response_code", ::google::api::LabelDescriptor_ValueType_STRING,
        SupportedLabel::USER, set_response_code,
    },
    {
        "/response_code_class", ::google::api::LabelDescriptor::STRING,
        SupportedLabel::USER, set_response_code_class,
    },
    {
        "/status_code", ::google::api::LabelDescriptor_ValueType_STRING,
        SupportedLabel::USER, set_status_code,
    },
    {
        "appengine.googleapis.com/clone_id",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::USER,
        nullptr,
    },
    {
        "appengine.googleapis.com/module_id",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::USER,
        nullptr,
    },
    {
        "appengine.googleapis.com/replica_index",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::USER,
        nullptr,
    },
    {
        "appengine.googleapis.com/version_id",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::USER,
        nullptr,
    },
    {
        "cloud.googleapis.com/location",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        set_location,
    },
    {
        "cloud.googleapis.com/project",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        nullptr,
    },
    {
        "cloud.googleapis.com/region",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        nullptr,
    },
    {
        "cloud.googleapis.com/resource_id",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::USER,
        nullptr,
    },
    {
        "cloud.googleapis.com/resource_type",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::USER,
        nullptr,
    },
    {
        "cloud.googleapis.com/service",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        nullptr,
    },
    {
        "cloud.googleapis.com/zone",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        nullptr,
    },
    {
        "cloud.googleapis.com/uid",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        nullptr,
    },
    {
        "serviceruntime.googleapis.com/api_method",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::USER,
        set_api_method,
    },
    {
        "serviceruntime.googleapis.com/api_version",
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::USER,
        set_api_version,
    },
    {
        kServiceControlCallerIp,
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        nullptr,
    },
    {
        kServiceControlReferer, ::google::api::LabelDescriptor_ValueType_STRING,
        SupportedLabel::SYSTEM, nullptr,
    },
    {
        kServiceControlServiceAgent,
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        set_service_agent,
    },
    {
        kServiceControlUserAgent,
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        set_user_agent,
    },
    {
        kServiceControlPlatform,
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        set_platform,
    },
    {
        kServiceControlBackendProtocol,
        ::google::api::LabelDescriptor_ValueType_STRING, SupportedLabel::SYSTEM,
        set_backend_protocol,
    },
};

const int supported_labels_count =
    sizeof(supported_labels) / sizeof(supported_labels[0]);

// Supported intrinsic labels:
// "servicecontrol.googleapis.com/operation_name": Operation.operation_name
// "servicecontrol.googleapis.com/consumer_id": Operation.consumer_id

// Unsupported service control labels:
// "servicecontrol.googleapis.com/android_package_name"
// "servicecontrol.googleapis.com/android_cert_fingerprint"
// "servicecontrol.googleapis.com/ios_bundle_id"
// "servicecontrol.googleapis.com/credential_project_number"

// Define Service Control constant strings
const char kConsumerIdApiKey[] = "api_key:";
const char kConsumerIdProject[] = "project:";

// Following names for for Log struct_playload field names:
const char kLogFieldNameTimestamp[] = "timestamp";
const char kLogFieldNameApiName[] = "api_name";
const char kLogFieldNameApiVersion[] = "api_version";
const char kLogFieldNameApiMethod[] = "api_method";
const char kLogFieldNameApiKey[] = "api_key";
const char kLogFieldNameProducerProjectId[] = "producer_project_id";
const char kLogFieldNameReferer[] = "referer";
const char kLogFieldNameLocation[] = "location";
const char kLogFieldNameRequestSize[] = "request_size_in_bytes";
const char kLogFieldNameResponseSize[] = "response_size_in_bytes";
const char kLogFieldNameHttpMethod[] = "http_method";
const char kLogFieldNameHttpResponseCode[] = "http_response_code";
const char kLogFieldNameLogMessage[] = "log_message";
const char kLogFieldNameRequestLatency[] = "request_latency_in_ms";
const char kLogFieldNameUrl[] = "url";
const char kLogFieldNameErrorCause[] = "error_cause";

// Convert timestamp from time_point to Timestamp
Timestamp CreateTimestamp(std::chrono::system_clock::time_point tp) {
  Timestamp time_stamp;
  long long nanos = std::chrono::duration_cast<std::chrono::nanoseconds>(
                        tp.time_since_epoch())
                        .count();

  time_stamp.set_seconds(nanos / 1000000000);
  time_stamp.set_nanos(nanos % 1000000000);
  return time_stamp;
}

Timestamp GetCurrentTimestamp() {
  return CreateTimestamp(std::chrono::system_clock::now());
}

Status VerifyRequiredCheckFields(const OperationInfo& info) {
  if (info.operation_id.empty()) {
    return Status(Code::INVALID_ARGUMENT, "operation_id is required.",
                  Status::SERVICE_CONTROL);
  }
  if (info.operation_name.empty()) {
    return Status(Code::INVALID_ARGUMENT, "operation_name is required.",
                  Status::SERVICE_CONTROL);
  }
  return Status::OK;
}

Status VerifyRequiredReportFields(const OperationInfo& info) {
  return Status::OK;
}

void SetOperationCommonFields(const OperationInfo& info,
                              const Timestamp& current_time, Operation* op) {
  if (!info.operation_id.empty()) {
    op->set_operation_id(info.operation_id);
  }
  if (!info.operation_name.empty()) {
    op->set_operation_name(info.operation_name);
  }
  if (!info.api_key.empty()) {
    op->set_consumer_id(std::string(kConsumerIdApiKey) +
                        std::string(info.api_key));
  }
  *op->mutable_start_time() = current_time;
  *op->mutable_end_time() = current_time;
}

void FillLogEntry(const ReportRequestInfo& info, const std::string& name,
                  const Timestamp& current_time, LogEntry* log_entry) {
  log_entry->set_name(name);
  *log_entry->mutable_timestamp() = current_time;
  auto severity = (info.response_code >= 400) ? google::logging::type::ERROR
                                              : google::logging::type::INFO;
  log_entry->set_severity(severity);

  auto* fields = log_entry->mutable_struct_payload()->mutable_fields();
  (*fields)[kLogFieldNameTimestamp].set_number_value(
      (double)current_time.seconds() +
      (double)current_time.nanos() / (double)1000000000.0);
  if (!info.producer_project_id.empty()) {
    (*fields)[kLogFieldNameProducerProjectId].set_string_value(
        info.producer_project_id);
  }
  if (!info.api_key.empty()) {
    (*fields)[kLogFieldNameApiKey].set_string_value(info.api_key);
  }
  if (!info.referer.empty()) {
    (*fields)[kLogFieldNameReferer].set_string_value(info.referer);
  }
  if (!info.api_name.empty()) {
    (*fields)[kLogFieldNameApiName].set_string_value(info.api_name);
  }
  if (!info.api_version.empty()) {
    (*fields)[kLogFieldNameApiVersion].set_string_value(info.api_version);
  }
  if (!info.url.empty()) {
    (*fields)[kLogFieldNameUrl].set_string_value(info.url);
  }
  if (!info.api_method.empty()) {
    (*fields)[kLogFieldNameApiMethod].set_string_value(info.api_method);
  }
  if (!info.location.empty()) {
    (*fields)[kLogFieldNameLocation].set_string_value(info.location);
  }
  if (!info.log_message.empty()) {
    (*fields)[kLogFieldNameLogMessage].set_string_value(info.log_message);
  }

  (*fields)[kLogFieldNameHttpResponseCode].set_number_value(info.response_code);

  if (info.request_size >= 0) {
    (*fields)[kLogFieldNameRequestSize].set_number_value(info.request_size);
  }
  if (info.response_size >= 0) {
    (*fields)[kLogFieldNameResponseSize].set_number_value(info.response_size);
  }
  if (info.latency.request_time_ms >= 0) {
    (*fields)[kLogFieldNameRequestLatency].set_number_value(
        info.latency.request_time_ms);
  }
  if (!info.method.empty()) {
    (*fields)[kLogFieldNameHttpMethod].set_string_value(info.method);
  }
  if (info.response_code >= 400) {
    (*fields)[kLogFieldNameErrorCause].set_string_value(
        Status::ErrorCauseToString(info.status.error_cause()));
  }
}

template <class Element>
std::vector<const Element*> FilterPointers(
    const Element* first, const Element* last,
    std::function<bool(const Element*)> pred) {
  std::vector<const Element*> filtered;
  while (first < last) {
    if (pred(first)) {
      filtered.push_back(first);
    }
    first++;
  }
  return filtered;
}

}  // namespace

Proto::Proto(const std::set<std::string>& logs, const std::string& service_name,
             const std::string& service_config_id)
    : logs_(logs.begin(), logs.end()),
      metrics_(FilterPointers<SupportedMetric>(
          supported_metrics, supported_metrics + supported_metrics_count,
          [](const struct SupportedMetric* m) { return m->set != nullptr; })),
      labels_(FilterPointers<SupportedLabel>(
          supported_labels, supported_labels + supported_labels_count,
          [](const struct SupportedLabel* l) { return l->set != nullptr; })),
      service_name_(service_name),
      service_config_id_(service_config_id) {}

Proto::Proto(const std::set<std::string>& logs,
             const std::set<std::string>& metrics,
             const std::set<std::string>& labels,
             const std::string& service_name,
             const std::string& service_config_id)
    : logs_(logs.begin(), logs.end()),
      metrics_(FilterPointers<SupportedMetric>(
          supported_metrics, supported_metrics + supported_metrics_count,
          [&metrics](const struct SupportedMetric* m) {
            return m->set && metrics.find(m->name) != metrics.end();
          })),
      labels_(FilterPointers<SupportedLabel>(
          supported_labels, supported_labels + supported_labels_count,
          [&labels](const struct SupportedLabel* l) {
            return l->set && (l->kind == SupportedLabel::SYSTEM ||
                              labels.find(l->name) != labels.end());
          })),
      service_name_(service_name),
      service_config_id_(service_config_id) {}

utils::Status Proto::FillAllocateQuotaRequest(
    const QuotaRequestInfo& info,
    ::google::api::servicecontrol::v1::AllocateQuotaRequest* request) {
  ::google::api::servicecontrol::v1::QuotaOperation* operation =
      request->mutable_allocate_operation();

  // service_name
  request->set_service_name(service_name_);
  // service_config_id
  request->set_service_config_id(service_config_id_);

  // allocate_operation.operation_id
  if (!info.operation_id.empty()) {
    operation->set_operation_id(info.operation_id);
  }
  // allocate_operation.method_name
  if (!info.method_name.empty()) {
    operation->set_method_name(info.method_name);
  }
  // allocate_operation.consumer_id
  if (!info.api_key.empty()) {
    operation->set_consumer_id(std::string(kConsumerIdApiKey) +
                               std::string(info.api_key));
  } else if (!info.producer_project_id.empty()) {
    operation->set_consumer_id(std::string(kConsumerIdProject) +
                               std::string(info.producer_project_id));
  }

  // allocate_operation.quota_mode
  operation->set_quota_mode(
      ::google::api::servicecontrol::v1::QuotaOperation_QuotaMode::
          QuotaOperation_QuotaMode_BEST_EFFORT);

  // allocate_operation.labels
  auto* labels = operation->mutable_labels();
  if (!info.client_ip.empty()) {
    (*labels)[kServiceControlCallerIp] = info.client_ip;
  }

  if (!info.referer.empty()) {
    (*labels)[kServiceControlReferer] = info.referer;
  }
  (*labels)[kServiceControlUserAgent] = kUserAgent;
  (*labels)[kServiceControlServiceAgent] =
      kServiceAgentPrefix + utils::Version::instance().get();

  if (info.metric_cost_vector) {
    for (auto metric : *info.metric_cost_vector) {
      MetricValueSet* value_set = operation->add_quota_metrics();
      value_set->set_metric_name(metric.first);
      MetricValue* value = value_set->add_metric_values();
      const auto& cost = metric.second;
      value->set_int64_value(cost <= 0 ? 1 : cost);
    }
  }

  return Status::OK;
}

Status Proto::FillCheckRequest(const CheckRequestInfo& info,
                               CheckRequest* request) {
  Status status = VerifyRequiredCheckFields(info);
  if (!status.ok()) {
    return status;
  }
  request->set_service_name(service_name_);
  request->set_service_config_id(service_config_id_);

  Timestamp current_time = GetCurrentTimestamp();
  Operation* op = request->mutable_operation();
  SetOperationCommonFields(info, current_time, op);

  auto* labels = op->mutable_labels();
  if (!info.client_ip.empty()) {
    (*labels)[kServiceControlCallerIp] = info.client_ip;
  }
  if (!info.referer.empty()) {
    (*labels)[kServiceControlReferer] = info.referer;
  }
  (*labels)[kServiceControlUserAgent] = kUserAgent;
  (*labels)[kServiceControlServiceAgent] =
      kServiceAgentPrefix + utils::Version::instance().get();

  if (!info.android_package_name.empty()) {
    (*labels)[kServiceControlAndroidPackageName] = info.android_package_name;
  }
  if (!info.android_cert_fingerprint.empty()) {
    (*labels)[kServiceControlAndroidCertFingerprint] =
        info.android_cert_fingerprint;
  }
  if (!info.ios_bundle_id.empty()) {
    (*labels)[kServiceControlIosBundleId] = info.ios_bundle_id;
  }

  return Status::OK;
}

Status Proto::FillReportRequest(const ReportRequestInfo& info,
                                ReportRequest* request) {
  Status status = VerifyRequiredReportFields(info);
  if (!status.ok()) {
    return status;
  }
  request->set_service_name(service_name_);
  request->set_service_config_id(service_config_id_);

  Timestamp current_time = GetCurrentTimestamp();
  Operation* op = request->add_operations();
  SetOperationCommonFields(info, current_time, op);

  // Only populate metrics if we can associate them with a method/operation.
  if (!info.operation_id.empty() && !info.operation_name.empty()) {
    Map<std::string, std::string>* labels = op->mutable_labels();
    // Set all labels.
    for (auto it = labels_.begin(), end = labels_.end(); it != end; it++) {
      const SupportedLabel* l = *it;
      if (l->set) {
        status = (l->set)(*l, info, labels);
        if (!status.ok()) return status;
      }
    }

    // Not to send consumer metrics if api_key is empty.
    // api_key is empty in one of following cases:
    // 1) api_key is not provided,
    // 2) api_key is invalid determined by the server from the Check call.
    // 3) the service is not activated for the consumer project.
    bool send_consumer_metric = !info.api_key.empty();

    // Populate all metrics.
    for (auto it = metrics_.begin(), end = metrics_.end(); it != end; it++) {
      const SupportedMetric* m = *it;
      if (send_consumer_metric || m->mark != SupportedMetric::CONSUMER) {
        if (m->set) {
          if ((info.is_first_report && m->tag == SupportedMetric::START) ||
              (info.is_final_report &&
               (m->tag == SupportedMetric::FINAL ||
                m->tag == SupportedMetric::INTERMEDIATE)) ||
              (!info.is_final_report &&
               m->tag == SupportedMetric::INTERMEDIATE)) {
            status = (m->set)(*m, info, op);
            if (!status.ok()) return status;
          }
        }
      }
    }
  }

  // Fill log entries.
  if (info.is_final_report) {
    for (auto it = logs_.begin(), end = logs_.end(); it != end; it++) {
      FillLogEntry(info, *it, current_time, op->add_log_entries());
    }
  }

  return Status::OK;
}

Status Proto::ConvertAllocateQuotaResponse(
    const ::google::api::servicecontrol::v1::AllocateQuotaResponse& response,
    const std::string& service_name) {
  // response.operation_id()
  if (response.allocate_errors().size() == 0) {
    return Status::OK;
  }

  const ::google::api::servicecontrol::v1::QuotaError& error =
      response.allocate_errors().Get(0);

  switch (error.code()) {
    case ::google::api::servicecontrol::v1::QuotaError::UNSPECIFIED:
      // This is never used.
      break;

    case ::google::api::servicecontrol::v1::QuotaError::RESOURCE_EXHAUSTED:
      // Quota allocation failed.
      // Same as [google.rpc.Code.RESOURCE_EXHAUSTED][].
      return Status(Code::RESOURCE_EXHAUSTED, error.description());

    case ::google::api::servicecontrol::v1::QuotaError::PROJECT_SUSPENDED:
    // Consumer project has been suspended.
    case ::google::api::servicecontrol::v1::QuotaError::SERVICE_NOT_ENABLED:
    // Consumer has not enabled the service.
    case ::google::api::servicecontrol::v1::QuotaError::BILLING_NOT_ACTIVE:
    // Consumer cannot access the service because billing is disabled.
    case ::google::api::servicecontrol::v1::QuotaError::IP_ADDRESS_BLOCKED:
    // IP address of the consumer is invalid for the specific consumer
    // project.
    case ::google::api::servicecontrol::v1::QuotaError::REFERER_BLOCKED:
    // Referer address of the consumer request is invalid for the specific
    // consumer project.
    case ::google::api::servicecontrol::v1::QuotaError::CLIENT_APP_BLOCKED:
      // Client application of the consumer request is invalid for the
      // specific consumer project.
      return Status(Code::PERMISSION_DENIED, error.description());

    case ::google::api::servicecontrol::v1::QuotaError::PROJECT_DELETED:
    // Consumer's project has been marked as deleted (soft deletion).
    case ::google::api::servicecontrol::v1::QuotaError::PROJECT_INVALID:
    // Consumer's project number or ID does not represent a valid project.
    case ::google::api::servicecontrol::v1::QuotaError::API_KEY_INVALID:
    // Specified API key is invalid.
    case ::google::api::servicecontrol::v1::QuotaError::API_KEY_EXPIRED:
      // Specified API Key has expired.
      return Status(Code::INVALID_ARGUMENT, error.description());

    case ::google::api::servicecontrol::v1::QuotaError::
        PROJECT_STATUS_UNVAILABLE:
    // The backend server for looking up project id/number is unavailable.
    case ::google::api::servicecontrol::v1::QuotaError::
        SERVICE_STATUS_UNAVAILABLE:
    // The backend server for checking service status is unavailable.
    case ::google::api::servicecontrol::v1::QuotaError::
        BILLING_STATUS_UNAVAILABLE:
    // The backend server for checking billing status is unavailable.
    // Fail open for internal server errors per recommendation
    case ::google::api::servicecontrol::v1::QuotaError::
        QUOTA_SYSTEM_UNAVAILABLE:
      // The backend server for checking quota limits is unavailable.
      return Status::OK;

    default:
      return Status(Code::INTERNAL, error.description());
  }

  return Status::OK;
}

Status Proto::ConvertCheckResponse(const CheckResponse& check_response,
                                   const std::string& service_name,
                                   CheckResponseInfo* check_response_info) {
  if (check_response.check_errors().size() == 0) {
    return Status::OK;
  }

  // TODO: aggregate status responses for all errors (including error.detail)
  // TODO: report a detailed status to the producer project, but hide it from
  // consumer
  // TODO: unless they are the same entity
  const CheckError& error = check_response.check_errors(0);
  switch (error.code()) {
    case CheckError::NOT_FOUND:  // The consumer's project id is not found.
      return Status(Code::INVALID_ARGUMENT,
                    "Client project not found. Please pass a valid project.",
                    Status::SERVICE_CONTROL);
    case CheckError::API_KEY_NOT_FOUND:
      if (check_response_info) check_response_info->is_api_key_valid = false;
      return Status(Code::INVALID_ARGUMENT,
                    "API key not found. Please pass a valid API key.",
                    Status::SERVICE_CONTROL);
    case CheckError::API_KEY_EXPIRED:
      if (check_response_info) check_response_info->is_api_key_valid = false;
      return Status(Code::INVALID_ARGUMENT,
                    "API key expired. Please renew the API key.",
                    Status::SERVICE_CONTROL);
    case CheckError::API_KEY_INVALID:
      if (check_response_info) check_response_info->is_api_key_valid = false;
      return Status(Code::INVALID_ARGUMENT,
                    "API key not valid. Please pass a valid API key.",
                    Status::SERVICE_CONTROL);
    case CheckError::SERVICE_NOT_ACTIVATED:
      if (check_response_info)
        check_response_info->service_is_activated = false;
      return Status(Code::PERMISSION_DENIED,
                    std::string("API ") + service_name +
                        " is not enabled for the project.",
                    Status::SERVICE_CONTROL);
    case CheckError::PERMISSION_DENIED:
      return Status(Code::PERMISSION_DENIED, "Permission denied.",
                    Status::SERVICE_CONTROL);
    case CheckError::IP_ADDRESS_BLOCKED:
      return Status(Code::PERMISSION_DENIED, "IP address blocked.",
                    Status::SERVICE_CONTROL);
    case CheckError::REFERER_BLOCKED:
      return Status(Code::PERMISSION_DENIED, "Referer blocked.",
                    Status::SERVICE_CONTROL);
    case CheckError::CLIENT_APP_BLOCKED:
      return Status(Code::PERMISSION_DENIED, "Client application blocked.",
                    Status::SERVICE_CONTROL);
    case CheckError::PROJECT_DELETED:
      return Status(Code::PERMISSION_DENIED, "Project has been deleted.",
                    Status::SERVICE_CONTROL);
    case CheckError::PROJECT_INVALID:
      return Status(Code::INVALID_ARGUMENT,
                    "Client project not valid. Please pass a valid project.",
                    Status::SERVICE_CONTROL);
    case CheckError::BILLING_DISABLED:
      return Status(Code::PERMISSION_DENIED,
                    std::string("API ") + service_name +
                        " has billing disabled. Please enable it.",
                    Status::SERVICE_CONTROL);
    case CheckError::NAMESPACE_LOOKUP_UNAVAILABLE:
    case CheckError::SERVICE_STATUS_UNAVAILABLE:
    case CheckError::BILLING_STATUS_UNAVAILABLE:
      // Fail open for internal server errors per recommendation
      return Status::OK;
    default:
      return Status(
          Code::INTERNAL,
          std::string("Request blocked due to unsupported error code: ") +
              std::to_string(error.code()),
          Status::SERVICE_CONTROL);
  }
  return Status::OK;
}

bool Proto::IsMetricSupported(const ::google::api::MetricDescriptor& metric) {
  for (int i = 0; i < supported_metrics_count; i++) {
    const SupportedMetric& m = supported_metrics[i];
    if (metric.name() == m.name && metric.metric_kind() == m.metric_kind &&
        metric.value_type() == m.value_type) {
      return true;
    }
  }
  return false;
}

bool Proto::IsLabelSupported(const ::google::api::LabelDescriptor& label) {
  for (int i = 0; i < supported_labels_count; i++) {
    const SupportedLabel& l = supported_labels[i];
    if (label.key() == l.name && label.value_type() == l.value_type) {
      return true;
    }
  }
  return false;
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
