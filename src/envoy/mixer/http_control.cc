/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#include "src/envoy/mixer/http_control.h"

#include "common/common/base64.h"
#include "common/common/utility.h"
#include "common/http/utility.h"

#include "src/envoy/mixer/string_map.pb.h"
#include "src/envoy/mixer/utils.h"

using ::google::protobuf::util::Status;
using ::istio::mixer_client::Attributes;
using ::istio::mixer_client::DoneFunc;

namespace Http {
namespace Mixer {
namespace {

// Define attribute names
const std::string kOriginUser = "origin.user";

const std::string kRequestHeaders = "request.headers";
const std::string kRequestHost = "request.host";
const std::string kRequestPath = "request.path";
const std::string kRequestSize = "request.size";
const std::string kRequestTime = "request.time";

const std::string kResponseHeaders = "response.headers";
const std::string kResponseHttpCode = "response.http.code";
const std::string kResponseLatency = "response.latency";
const std::string kResponseSize = "response.size";
const std::string kResponseTime = "response.time";

void SetStringAttribute(const std::string& name, const std::string& value,
                        Attributes* attr) {
  if (!value.empty()) {
    attr->attributes[name] = Attributes::StringValue(value);
  }
}

std::map<std::string, std::string> ExtractHeaders(const HeaderMap& header_map) {
  std::map<std::string, std::string> headers;
  header_map.iterate(
      [](const HeaderEntry& header, void* context) {
        std::map<std::string, std::string>* header_map =
            static_cast<std::map<std::string, std::string>*>(context);
        (*header_map)[header.key().c_str()] = header.value().c_str();
      },
      &headers);
  return headers;
}

void FillRequestHeaderAttributes(const HeaderMap& header_map,
                                 Attributes* attr) {
  SetStringAttribute(kRequestPath, header_map.Path()->value().c_str(), attr);
  SetStringAttribute(kRequestHost, header_map.Host()->value().c_str(), attr);
  attr->attributes[kRequestTime] =
      Attributes::TimeValue(std::chrono::system_clock::now());
  attr->attributes[kRequestHeaders] =
      Attributes::StringMapValue(ExtractHeaders(header_map));
}

void FillResponseHeaderAttributes(const HeaderMap* header_map,
                                  Attributes* attr) {
  if (header_map) {
    attr->attributes[kResponseHeaders] =
        Attributes::StringMapValue(ExtractHeaders(*header_map));
  }
  attr->attributes[kResponseTime] =
      Attributes::TimeValue(std::chrono::system_clock::now());
}

void FillRequestInfoAttributes(const AccessLog::RequestInfo& info,
                               int check_status_code, Attributes* attr) {
  if (info.bytesReceived() >= 0) {
    attr->attributes[kRequestSize] =
        Attributes::Int64Value(info.bytesReceived());
  }
  if (info.bytesSent() >= 0) {
    attr->attributes[kResponseSize] = Attributes::Int64Value(info.bytesSent());
  }

  attr->attributes[kResponseLatency] = Attributes::DurationValue(
      std::chrono::duration_cast<std::chrono::nanoseconds>(info.duration()));

  if (info.responseCode().valid()) {
    attr->attributes[kResponseHttpCode] =
        Attributes::Int64Value(info.responseCode().value());
  } else {
    attr->attributes[kResponseHttpCode] =
        Attributes::Int64Value(check_status_code);
  }
}

}  // namespace

HttpControl::HttpControl(const std::string& mixer_server,
                         std::map<std::string, std::string>&& attributes)
    : config_attributes_(std::move(attributes)) {
  ::istio::mixer_client::MixerClientOptions options;
  options.mixer_server = mixer_server;
  mixer_client_ = ::istio::mixer_client::CreateMixerClient(options);
}

void HttpControl::FillCheckAttributes(HeaderMap& header_map, Attributes* attr) {
  // Extract attributes from x-istio-attributes header
  const HeaderEntry* entry = header_map.get(Utils::kIstioAttributeHeader);
  if (entry) {
    ::istio::proxy::mixer::StringMap pb;
    std::string str(entry->value().c_str(), entry->value().size());
    pb.ParseFromString(Base64::decode(str));
    for (const auto& it : pb.map()) {
      SetStringAttribute(it.first, it.second, attr);
    }
    header_map.remove(Utils::kIstioAttributeHeader);
  }

  FillRequestHeaderAttributes(header_map, attr);

  for (const auto& attribute : config_attributes_) {
    SetStringAttribute(attribute.first, attribute.second, attr);
  }
}

void HttpControl::Check(HttpRequestDataPtr request_data, HeaderMap& headers,
                        std::string origin_user, DoneFunc on_done) {
  FillCheckAttributes(headers, &request_data->attributes);
  SetStringAttribute(kOriginUser, origin_user, &request_data->attributes);
  log().debug("Send Check: {}", request_data->attributes.DebugString());
  mixer_client_->Check(request_data->attributes, on_done);
}

void HttpControl::Report(HttpRequestDataPtr request_data,
                         const HeaderMap* response_headers,
                         const AccessLog::RequestInfo& request_info,
                         int check_status, DoneFunc on_done) {
  // Use all Check attributes for Report.
  // Add additional Report attributes.
  FillResponseHeaderAttributes(response_headers, &request_data->attributes);

  FillRequestInfoAttributes(request_info, check_status,
                            &request_data->attributes);
  log().debug("Send Report: {}", request_data->attributes.DebugString());
  mixer_client_->Report(request_data->attributes, on_done);
}

}  // namespace Mixer
}  // namespace Http
