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
#include "src/api_manager/service_control/aggregated.h"

#include <sstream>
#include <typeinfo>
#include "src/api_manager/service_control/logs_metrics_loader.h"

using ::google::api::servicecontrol::v1::CheckRequest;
using ::google::api::servicecontrol::v1::CheckResponse;
using ::google::api::servicecontrol::v1::ReportRequest;
using ::google::api::servicecontrol::v1::ReportResponse;
using ::google::api_manager::proto::ServerConfig;
using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

using ::google::service_control_client::CheckAggregationOptions;
using ::google::service_control_client::ReportAggregationOptions;
using ::google::service_control_client::ServiceControlClient;
using ::google::service_control_client::ServiceControlClientOptions;
using ::google::service_control_client::TransportDoneFunc;

namespace google {
namespace api_manager {
namespace service_control {

namespace {

// Default config for check aggregator
const int kCheckAggregationEntries = 10000;
// Check doesn't support quota yet. It is safe to increase
// the cache life of check results.
// Cache life is 5 minutes. It will be refreshed every minute.
const int kCheckAggregationFlushIntervalMs = 60000;
const int kCheckAggregationExpirationMs = 300000;

// Default config for report aggregator
const int kReportAggregationEntries = 10000;
const int kReportAggregationFlushIntervalMs = 1000;

// The default connection timeout for check requests.
const int kCheckDefaultTimeoutInMs = 5000;
// The default connection timeout for report requests.
const int kReportDefaultTimeoutInMs = 15000;

// The maximum protobuf pool size. All usages of pool alloc() and free() are
// within a function frame. If no con-current usage, pool size of 1 is enough.
// This number should correspond to maximum of concurrent calls.
const int kProtoPoolMaxSize = 100;

// Defines protobuf content type.
const char application_proto[] = "application/x-protobuf";

// The service_control service name. used for as audience to generate JWT token.
const char servicecontrol_service[] =
    "/google.api.servicecontrol.v1.ServiceController";

// Generates CheckAggregationOptions.
CheckAggregationOptions GetCheckAggregationOptions(
    const ServerConfig* server_config) {
  if (server_config && server_config->has_service_control_config() &&
      server_config->service_control_config().has_check_aggregator_config()) {
    const auto& check_config =
        server_config->service_control_config().check_aggregator_config();
    return CheckAggregationOptions(check_config.cache_entries(),
                                   check_config.flush_interval_ms(),
                                   check_config.response_expiration_ms());
  }
  return CheckAggregationOptions(kCheckAggregationEntries,
                                 kCheckAggregationFlushIntervalMs,
                                 kCheckAggregationExpirationMs);
}

// Generates ReportAggregationOptions.
ReportAggregationOptions GetReportAggregationOptions(
    const ServerConfig* server_config) {
  if (server_config && server_config->has_service_control_config() &&
      server_config->service_control_config().has_report_aggregator_config()) {
    const auto& report_config =
        server_config->service_control_config().report_aggregator_config();
    return ReportAggregationOptions(report_config.cache_entries(),
                                    report_config.flush_interval_ms());
  }
  return ReportAggregationOptions(kReportAggregationEntries,
                                  kReportAggregationFlushIntervalMs);
}

}  // namespace

template <class Type>
std::unique_ptr<Type> Aggregated::ProtoPool<Type>::Alloc() {
  std::lock_guard<std::mutex> lock(mutex_);
  if (!pool_.empty()) {
    auto item = std::move(pool_.front());
    pool_.pop_front();
    item->Clear();
    return item;
  } else {
    return std::unique_ptr<Type>(new Type);
  }
}

template <class Type>
void Aggregated::ProtoPool<Type>::Free(std::unique_ptr<Type> item) {
  std::lock_guard<std::mutex> lock(mutex_);
  if (pool_.size() < kProtoPoolMaxSize) {
    pool_.push_back(std::move(item));
  }
}

Aggregated::Aggregated(const ::google::api::Service& service,
                       const ServerConfig* server_config,
                       ApiManagerEnvInterface* env,
                       auth::ServiceAccountToken* sa_token,
                       const std::set<std::string>& logs,
                       const std::set<std::string>& metrics,
                       const std::set<std::string>& labels)
    : service_(&service),
      server_config_(server_config),
      env_(env),
      sa_token_(sa_token),
      service_control_proto_(logs, metrics, labels, service.name(),
                             service.id()),
      url_(service_, server_config),
      mismatched_check_config_id(service.id()),
      mismatched_report_config_id(service.id()),
      max_report_size_(0) {
  if (sa_token_) {
    sa_token_->SetAudience(
        auth::ServiceAccountToken::JWT_TOKEN_FOR_SERVICE_CONTROL,
        url_.service_control() + servicecontrol_service);
  }
}

Aggregated::Aggregated(const std::set<std::string>& logs,
                       ApiManagerEnvInterface* env,
                       std::unique_ptr<ServiceControlClient> client)
    : service_(nullptr),
      server_config_(nullptr),
      env_(env),
      sa_token_(nullptr),
      service_control_proto_(logs, "", ""),
      url_(service_, server_config_),
      client_(std::move(client)),
      max_report_size_(0) {}

Aggregated::~Aggregated() {}

Status Aggregated::Init() {
  // Init() can be called repeatedly.
  if (client_) {
    return Status::OK;
  }

  // It is too early to create client_ at constructor.
  // Client creation is calling env->StartPeriodicTimer.
  // env->StartPeriodicTimer doens't work at constructor.
  ServiceControlClientOptions options(
      GetCheckAggregationOptions(server_config_),
      GetReportAggregationOptions(server_config_));

  std::stringstream ss;
  ss << "Check_aggregation_options: "
     << "num_entries: " << options.check_options.num_entries
     << ", flush_interval_ms: " << options.check_options.flush_interval_ms
     << ", expiration_ms: " << options.check_options.expiration_ms
     << ", Report_aggregation_options: "
     << "num_entries: " << options.report_options.num_entries
     << ", flush_interval_ms: " << options.report_options.flush_interval_ms;
  env_->LogInfo(ss.str().c_str());

  options.check_transport = [this](
      const CheckRequest& request, CheckResponse* response,
      TransportDoneFunc on_done) { Call(request, response, on_done, nullptr); };
  options.report_transport = [this](
      const ReportRequest& request, ReportResponse* response,
      TransportDoneFunc on_done) { Call(request, response, on_done, nullptr); };

  options.periodic_timer = [this](int interval_ms,
                                  std::function<void()> callback)
      -> std::unique_ptr<::google::service_control_client::PeriodicTimer> {
        return std::unique_ptr<::google::service_control_client::PeriodicTimer>(
            new ApiManagerPeriodicTimer(env_->StartPeriodicTimer(
                std::chrono::milliseconds(interval_ms), callback)));
      };
  client_ = ::google::service_control_client::CreateServiceControlClient(
      service_->name(), service_->id(), options);
  return Status::OK;
}

Status Aggregated::Close() {
  // Just destroy the client to flush all its cache.
  client_.reset();
  return Status::OK;
}

Status Aggregated::Report(const ReportRequestInfo& info) {
  if (!client_) {
    return Status(Code::INTERNAL, "Missing service control client");
  }
  auto request = report_pool_.Alloc();
  Status status = service_control_proto_.FillReportRequest(info, request.get());
  if (!status.ok()) {
    report_pool_.Free(std::move(request));
    return status;
  }
  ReportResponse* response = new ReportResponse;
  client_->Report(
      *request, response,
      [this, response](const ::google::protobuf::util::Status& status) {
        if (service_control_proto_.service_config_id() !=
            response->service_config_id()) {
          if (mismatched_report_config_id != response->service_config_id()) {
            env_->LogWarning(
                "Received non-matching report response service config ID: '" +
                response->service_config_id() + "', requested: '" +
                service_control_proto_.service_config_id() + "'");
            mismatched_report_config_id = response->service_config_id();
          }
        }

        if (!status.ok() && env_) {
          env_->LogError(std::string("Service control report failed. " +
                                     status.ToString()));
        }
        delete response;
      });
  // There is no reference to request anymore at this point and it is safe to
  // free request now.
  report_pool_.Free(std::move(request));
  return Status::OK;
}

void Aggregated::Check(
    const CheckRequestInfo& info, cloud_trace::CloudTraceSpan* parent_span,
    std::function<void(Status, const CheckResponseInfo&)> on_done) {
  std::shared_ptr<cloud_trace::CloudTraceSpan> trace_span(
      CreateChildSpan(parent_span, "CheckServiceControlCache"));
  CheckResponseInfo dummy_response_info;
  if (!client_) {
    on_done(Status(Code::INTERNAL, "Missing service control client"),
            dummy_response_info);
    return;
  }
  auto request = check_pool_.Alloc();
  Status status = service_control_proto_.FillCheckRequest(info, request.get());
  if (!status.ok()) {
    on_done(status, dummy_response_info);
    check_pool_.Free(std::move(request));
    return;
  }

  CheckResponse* response = new CheckResponse;
  bool allow_unregistered_calls = info.allow_unregistered_calls;

  auto check_on_done = [this, response, allow_unregistered_calls, on_done,
                        trace_span](
      const ::google::protobuf::util::Status& status) {
    TRACE(trace_span) << "Check returned with status: " << status.ToString();
    CheckResponseInfo response_info;

    if (service_control_proto_.service_config_id() !=
        response->service_config_id()) {
      if (mismatched_check_config_id != response->service_config_id()) {
        env_->LogWarning(
            "Received non-matching check response service config ID: '" +
            response->service_config_id() + "', requested: '" +
            service_control_proto_.service_config_id() + "'");
        mismatched_check_config_id = response->service_config_id();
      }
    }

    if (status.ok()) {
      Status status = Proto::ConvertCheckResponse(
          *response, service_control_proto_.service_name(), &response_info);
      // If allow_unregistered_calls is true, it is always OK to proceed.
      if (allow_unregistered_calls) {
        on_done(Status::OK, response_info);
      } else {
        on_done(status, response_info);
      }
    } else {
      // If allow_unregistered_calls is true, it is always OK to proceed.
      if (allow_unregistered_calls) {
        on_done(Status::OK, response_info);
      } else {
        on_done(Status(status.error_code(), status.error_message(),
                       Status::SERVICE_CONTROL),
                response_info);
      }
    }
    delete response;
  };

  client_->Check(
      *request, response, check_on_done,
      [trace_span, this](const CheckRequest& request, CheckResponse* response,
                         TransportDoneFunc on_done) {
        Call(request, response, on_done, trace_span.get());
      });
  // There is no reference to request anymore at this point and it is safe to
  // free request now.
  check_pool_.Free(std::move(request));
}

Status Aggregated::GetStatistics(Statistics* esp_stat) const {
  if (!client_) {
    return Status(Code::INTERNAL, "Missing service control client");
  }

  ::google::service_control_client::Statistics client_stat;
  ::google::protobuf::util::Status status =
      client_->GetStatistics(&client_stat);

  if (!status.ok()) {
    return Status::FromProto(status);
  }
  esp_stat->total_called_checks = client_stat.total_called_checks;
  esp_stat->send_checks_by_flush = client_stat.send_checks_by_flush;
  esp_stat->send_checks_in_flight = client_stat.send_checks_in_flight;
  esp_stat->total_called_reports = client_stat.total_called_reports;
  esp_stat->send_reports_by_flush = client_stat.send_reports_by_flush;
  esp_stat->send_reports_in_flight = client_stat.send_reports_in_flight;
  esp_stat->send_report_operations = client_stat.send_report_operations;
  esp_stat->max_report_size = max_report_size_;

  return Status::OK;
}

template <class RequestType, class ResponseType>
void Aggregated::Call(const RequestType& request, ResponseType* response,
                      TransportDoneFunc on_done,
                      cloud_trace::CloudTraceSpan* parent_span) {
  std::shared_ptr<cloud_trace::CloudTraceSpan> trace_span(
      CreateChildSpan(parent_span, "Call ServiceControl server"));
  std::unique_ptr<HTTPRequest> http_request(new HTTPRequest([response, on_done,
                                                             trace_span, this](
      Status status, std::map<std::string, std::string>&&, std::string&& body) {
    TRACE(trace_span) << "HTTP response status: " << status.ToString();
    if (status.ok()) {
      // Handle 200 response
      if (!response->ParseFromString(body)) {
        status =
            Status(Code::INVALID_ARGUMENT, std::string("Invalid response"));
      }
    } else {
      const std::string& url = typeid(RequestType) == typeid(CheckRequest)
                                   ? url_.check_url()
                                   : url_.report_url();
      env_->LogError(std::string("Failed to call ") + url + ", Error: " +
                     status.ToString() + ", Response body: " + body);

      // Handle NGX error as opposed to pass-through error code
      if (status.code() < 0) {
        status =
            Status(Code::UNAVAILABLE, "Failed to connect to service control");
      } else {
        status =
            Status(Code::UNAVAILABLE,
                   "Service control request failed with HTTP response code " +
                       std::to_string(status.code()));
      }
    }
    on_done(status.ToProto());
  }));

  bool is_check = (typeid(RequestType) == typeid(CheckRequest));
  const std::string& url = is_check ? url_.check_url() : url_.report_url();
  TRACE(trace_span) << "Http request URL: " << url;

  std::string request_body;
  request.SerializeToString(&request_body);

  if (!is_check && (request_body.size() > max_report_size_)) {
    max_report_size_ = request_body.size();
  }

  http_request->set_url(url)
      .set_method("POST")
      .set_auth_token(GetAuthToken())
      .set_header("Content-Type", application_proto)
      .set_body(request_body);

  // Set timeout on the request if it was so configured.
  if (is_check) {
    http_request->set_timeout_ms(kCheckDefaultTimeoutInMs);
  } else {
    http_request->set_timeout_ms(kReportDefaultTimeoutInMs);
  }
  if (server_config_ != nullptr &&
      server_config_->has_service_control_config()) {
    const auto& config = server_config_->service_control_config();
    if (is_check) {
      if (config.check_timeout_ms() > 0) {
        http_request->set_timeout_ms(config.check_timeout_ms());
      }
    } else {
      if (config.report_timeout_ms() > 0) {
        http_request->set_timeout_ms(config.report_timeout_ms());
      }
    }
  }

  env_->RunHTTPRequest(std::move(http_request));
}

const std::string& Aggregated::GetAuthToken() {
  if (sa_token_) {
    return sa_token_->GetAuthToken(
        auth::ServiceAccountToken::JWT_TOKEN_FOR_SERVICE_CONTROL);
  } else {
    static std::string empty;
    return empty;
  }
}

Interface* Aggregated::Create(const ::google::api::Service& service,
                              const ServerConfig* server_config,
                              ApiManagerEnvInterface* env,
                              auth::ServiceAccountToken* sa_token) {
  if (server_config &&
      server_config->service_control_config().force_disable()) {
    env->LogError("Service control is disabled.");
    return nullptr;
  }
  Url url(&service, server_config);
  if (url.service_control().empty()) {
    env->LogError(
        "Service control address is not specified. Disabling API management.");
    return nullptr;
  }
  std::set<std::string> logs, metrics, labels;
  Status s = LogsMetricsLoader::Load(service, &logs, &metrics, &labels);
  return new Aggregated(service, server_config, env, sa_token, logs, metrics,
                        labels);
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
