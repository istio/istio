/* Copyright 2016 Google Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
#include "contrib/endpoints/src/api_manager/mixer/mixer.h"

using ::google::api_manager::utils::Status;
using ::istio::mixer_client::Attributes;

namespace google {
namespace api_manager {
namespace mixer {
namespace {

const std::string kProxyPeerID = "Istio/Proxy";

const std::string kAttrNameServiceName = "serviceName";
const std::string kAttrNamePeerId = "peerId";
const std::string kAttrNameOperationId = "operationId";
const std::string kAttrNameOperationName = "operationName";
const std::string kAttrNameApiKey = "apiKey";
const std::string kAttrNameResponseCode = "responseCode";
const std::string kAttrNameURL = "url";
const std::string kAttrNameLocation = "location";
const std::string kAttrNameApiName = "apiName";
const std::string kAttrNameApiVersion = "apiVersion";
const std::string kAttrNameApiMethod = "apiMethod";
const std::string kAttrNameRequestSize = "requestSize";
const std::string kAttrNameResponseSize = "responseSize";
const std::string kAttrNameLogMessage = "logMessage";
const std::string kAttrNameResponseTime = "responseTime";
const std::string kAttrNameOriginIp = "originIp";
const std::string kAttrNameOriginHost = "originHost";

Attributes::Value StringValue(const std::string& str) {
  Attributes::Value v;
  v.type = Attributes::Value::STRING;
  v.str_v = str;
  return v;
}

Attributes::Value Int64Value(int64_t value) {
  Attributes::Value v;
  v.type = Attributes::Value::INT64;
  v.value.int64_v = value;
  return v;
}

}  // namespace

Mixer::Mixer(ApiManagerEnvInterface* env, const Config* config)
    : env_(env), config_(config) {}

Mixer::~Mixer() {}

Status Mixer::Init() {
  ::istio::mixer_client::MixerClientOptions options;
  options.mixer_server =
      config_->server_config()->mixer_options().mixer_server();
  mixer_client_ = ::istio::mixer_client::CreateMixerClient(options);
  return Status::OK;
}

Status Mixer::Close() { return Status::OK; }

void Mixer::FillCommonAttributes(const service_control::OperationInfo& info,
                                 ::istio::mixer_client::Attributes* attr) {
  attr->attributes[kAttrNameServiceName] = StringValue(config_->service_name());
  attr->attributes[kAttrNamePeerId] = StringValue(kProxyPeerID);

  if (!info.operation_id.empty()) {
    attr->attributes[kAttrNameOperationId] = StringValue(info.operation_id);
  }
  if (!info.operation_name.empty()) {
    attr->attributes[kAttrNameOperationName] = StringValue(info.operation_name);
  }
  if (!info.api_key.empty()) {
    attr->attributes[kAttrNameApiKey] = StringValue(info.api_key);
  }
  if (!info.client_ip.empty()) {
    attr->attributes[kAttrNameOriginIp] = StringValue(info.client_ip);
  }
  if (!info.client_host.empty()) {
    attr->attributes[kAttrNameOriginHost] = StringValue(info.client_host);
  }
}

void Mixer::FillCheckAttributes(const service_control::CheckRequestInfo& info,
                                ::istio::mixer_client::Attributes* attr) {
  FillCommonAttributes(info, attr);
}

void Mixer::FillReportAttributes(const service_control::ReportRequestInfo& info,
                                 ::istio::mixer_client::Attributes* attr) {
  FillCommonAttributes(info, attr);

  if (!info.url.empty()) {
    attr->attributes[kAttrNameURL] = StringValue(info.url);
  }
  if (!info.location.empty()) {
    attr->attributes[kAttrNameLocation] = StringValue(info.location);
  }

  if (!info.api_name.empty()) {
    attr->attributes[kAttrNameApiName] = StringValue(info.api_name);
  }
  if (!info.api_version.empty()) {
    attr->attributes[kAttrNameApiVersion] = StringValue(info.api_version);
  }
  if (!info.api_method.empty()) {
    attr->attributes[kAttrNameApiMethod] = StringValue(info.api_method);
  }

  if (!info.log_message.empty()) {
    attr->attributes[kAttrNameLogMessage] = StringValue(info.log_message);
  }

  attr->attributes[kAttrNameResponseCode] = Int64Value(info.response_code);
  if (info.request_size >= 0) {
    attr->attributes[kAttrNameRequestSize] = Int64Value(info.request_size);
  }
  if (info.response_size >= 0) {
    attr->attributes[kAttrNameResponseSize] = Int64Value(info.response_size);
  }

  if (info.latency.request_time_ms >= 0) {
    attr->attributes[kAttrNameResponseTime] =
        Int64Value(info.latency.request_time_ms);
  }
}

Status Mixer::Report(const service_control::ReportRequestInfo& info) {
  ::istio::mixer_client::Attributes attributes;
  FillReportAttributes(info, &attributes);
  env_->LogInfo("Send Report: ");
  mixer_client_->Report(
      attributes, [this](const ::google::protobuf::util::Status& status) {
        if (status.ok()) {
          env_->LogInfo("Report response: OK");
        } else {
          env_->LogError(std::string("Failed to call Mixer::report, Error: ") +
                         status.ToString());
        }
      });
  return Status::OK;
}

void Mixer::Check(
    const service_control::CheckRequestInfo& info,
    cloud_trace::CloudTraceSpan* parent_span,
    std::function<void(Status, const service_control::CheckResponseInfo&)>
        on_done) {
  ::istio::mixer_client::Attributes attributes;
  FillCheckAttributes(info, &attributes);
  env_->LogInfo("Send Check: ");
  mixer_client_->Check(
      attributes,
      [this, on_done](const ::google::protobuf::util::Status& status) {
        if (status.ok()) {
          env_->LogInfo("Check response: OK");
        } else {
          env_->LogError(std::string("Failed to call Mixer::check, Error: ") +
                         status.ToString());
        }
        service_control::CheckResponseInfo info;
        on_done(Status(status.error_code(), status.error_message(),
                       Status::SERVICE_CONTROL),
                info);
      });
}

Status Mixer::GetStatistics(service_control::Statistics* esp_stat) const {
  return Status::OK;
}

service_control::Interface* Mixer::Create(ApiManagerEnvInterface* env,
                                          const Config* config) {
  return new Mixer(env, config);
}

}  // namespace mixer
}  // namespace api_manager
}  // namespace google
