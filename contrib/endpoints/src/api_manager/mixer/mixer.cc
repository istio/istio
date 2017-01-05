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

#include <sstream>
#include "mixer/api/v1/service.pb.h"

using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;
using ::google::protobuf::Map;

namespace google {
namespace api_manager {
namespace mixer {
namespace {

const char kMixerServiceName[] = "istio.mixer.v1.Mixer";

enum AttributeIndex {
  ATTR_SERVICE_NAME = 0,
  ATTR_PEER_ID,
  ATTR_OPERATION_NAME,
  ATTR_API_KEY,
  ATTR_RESPONSE_CODE,
  ATTR_URL,
  ATTR_LOCATION,
  ATTR_API_NAME,
  ATTR_API_VERSION,
  ATTR_API_METHOD,
  ATTR_REQUEST_SIZE,
  ATTR_RESPONSE_SIZE,
  ATTR_LOG_MESSAGE,
};

struct AttributeDict {
  int index;
  std::string name;
} kAttributeNames[] = {
    {
        ATTR_SERVICE_NAME, "serviceName",
    },
    {
        ATTR_PEER_ID, "peerId",
    },
    {
        ATTR_OPERATION_NAME, "operationName",
    },
    {
        ATTR_API_KEY, "apiKey",
    },
    {
        ATTR_RESPONSE_CODE, "responseCode",
    },
    {
        ATTR_URL, "URL",
    },
    {
        ATTR_LOCATION, "location",
    },
    {
        ATTR_API_NAME, "apiName",
    },
    {
        ATTR_API_VERSION, "apiVersion",
    },
    {
        ATTR_API_METHOD, "apiMethod",
    },
    {
        ATTR_REQUEST_SIZE, "requestSize",
    },
    {
        ATTR_RESPONSE_SIZE, "responesSize",
    },
    {
        ATTR_LOG_MESSAGE, "logMessage",
    },
};

void SetAttributeDict(Map<int32_t, std::string>* dict) {
  for (auto attr : kAttributeNames) {
    (*dict)[attr.index] = attr.name;
  }
}

void CovertToPb(const service_control::CheckRequestInfo& info,
                const std::string& service_name,
                ::istio::mixer::v1::Attributes* attr) {
  SetAttributeDict(attr->mutable_dictionary());

  auto* str_attrs = attr->mutable_string_attributes();
  (*str_attrs)[ATTR_SERVICE_NAME] = service_name;
  (*str_attrs)[ATTR_PEER_ID] = "Proxy";
  (*str_attrs)[ATTR_OPERATION_NAME] = info.operation_name;
  (*str_attrs)[ATTR_API_KEY] = info.api_key;
}

void CovertToPb(const service_control::ReportRequestInfo& info,
                const std::string& service_name,
                ::istio::mixer::v1::Attributes* attr) {
  SetAttributeDict(attr->mutable_dictionary());

  auto* str_attrs = attr->mutable_string_attributes();
  (*str_attrs)[ATTR_SERVICE_NAME] = service_name;
  (*str_attrs)[ATTR_PEER_ID] = "Proxy";
  (*str_attrs)[ATTR_OPERATION_NAME] = info.operation_name;
  (*str_attrs)[ATTR_API_KEY] = info.api_key;

  (*str_attrs)[ATTR_URL] = info.url;
  (*str_attrs)[ATTR_LOCATION] = info.location;

  (*str_attrs)[ATTR_API_NAME] = info.api_name;
  (*str_attrs)[ATTR_API_VERSION] = info.api_version;
  (*str_attrs)[ATTR_API_METHOD] = info.api_method;

  (*str_attrs)[ATTR_LOG_MESSAGE] = info.log_message;

  auto* int_attrs = attr->mutable_int64_attributes();
  (*int_attrs)[ATTR_RESPONSE_CODE] = info.response_code;
  (*int_attrs)[ATTR_REQUEST_SIZE] = info.request_size;
  (*int_attrs)[ATTR_RESPONSE_SIZE] = info.response_size;
}

}  // namespace

Mixer::Mixer(ApiManagerEnvInterface* env, const Config* config)
    : env_(env), request_index_(0), config_(config) {}

Mixer::~Mixer() {}

Status Mixer::Init() { return Status::OK; }

Status Mixer::Close() { return Status::OK; }

Status Mixer::Report(const service_control::ReportRequestInfo& info) {
  std::unique_ptr<GRPCRequest> grpc_request(new GRPCRequest([this](
      Status status, std::string&& body) {
    if (status.ok()) {
      // Handle 200 response
      ::istio::mixer::v1::ReportResponse response;
      if (!response.ParseFromString(body)) {
        status =
            Status(Code::INVALID_ARGUMENT, std::string("Invalid response"));
        env_->LogError(std::string("Failed parse report response: ") + body);
      }
      env_->LogInfo(std::string("Report response: ") + response.DebugString());
    } else {
      env_->LogError(std::string("Failed to call Mixer::report, Error: ") +
                     status.ToString());
    }
  }));

  ::istio::mixer::v1::ReportRequest request;
  request.set_request_index(++request_index_);
  CovertToPb(info, config_->service_name(), request.mutable_attribute_update());
  env_->LogInfo(std::string("Send Report: ") + request.DebugString());

  std::string request_body;
  request.SerializeToString(&request_body);

  grpc_request
      ->set_server(config_->server_config()->mixer_options().mixer_server())
      .set_service(kMixerServiceName)
      .set_method("Report")
      .set_body(request_body);

  env_->RunGRPCRequest(std::move(grpc_request));
  return Status::OK;
}

void Mixer::Check(
    const service_control::CheckRequestInfo& info,
    cloud_trace::CloudTraceSpan* parent_span,
    std::function<void(Status, const service_control::CheckResponseInfo&)>
        on_done) {
  std::unique_ptr<GRPCRequest> grpc_request(new GRPCRequest([this, on_done](
      Status status, std::string&& body) {
    if (status.ok()) {
      // Handle 200 response
      ::istio::mixer::v1::CheckResponse response;
      if (!response.ParseFromString(body)) {
        status =
            Status(Code::INVALID_ARGUMENT, std::string("Invalid response"));
        env_->LogError(std::string("Failed parse check response: ") + body);
      }
      env_->LogInfo(std::string("Check response: ") + response.DebugString());
    } else {
      env_->LogError(std::string("Failed to call Mixer::check, Error: ") +
                     status.ToString());
    }
    service_control::CheckResponseInfo info;
    on_done(status, info);
  }));

  ::istio::mixer::v1::CheckRequest request;
  request.set_request_index(++request_index_);
  CovertToPb(info, config_->service_name(), request.mutable_attribute_update());
  env_->LogInfo(std::string("Send Check: ") + request.DebugString());

  std::string request_body;
  request.SerializeToString(&request_body);

  grpc_request
      ->set_server(config_->server_config()->mixer_options().mixer_server())
      .set_service(kMixerServiceName)
      .set_method("Check")
      .set_body(request_body);

  env_->RunGRPCRequest(std::move(grpc_request));
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
