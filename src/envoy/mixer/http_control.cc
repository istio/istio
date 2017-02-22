/*
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
#include "common/common/utility.h"
#include "common/http/utility.h"

using ::google::protobuf::util::Status;
using ::istio::mixer_client::Attributes;
using ::istio::mixer_client::DoneFunc;

namespace Http {
namespace Mixer {
namespace {

// Define lower case string for X-Forwarded-Host.
const LowerCaseString kHeaderNameXFH("x-forwarded-host", false);

const std::string kRequestHeaderPrefix = "request.headers.";
const std::string kResponseHeaderPrefix = "response.headers.";

// Define attribute names
const std::string kAttrNameRequestPath = "request.path";
const std::string kAttrNameRequestSize = "request.size";
const std::string kAttrNameResponseSize = "response.size";
const std::string kAttrNameResponseTime = "response.time";
const std::string kAttrNameOriginIp = "origin.ip";
const std::string kAttrNameOriginHost = "origin.host";
const std::string kResponseStatusCode = "response.status.code";

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

void SetStringAttribute(const std::string& name, const std::string& value,
                        Attributes* attr) {
  if (!value.empty()) {
    attr->attributes[name] = StringValue(value);
  }
}

std::string GetFirstForwardedFor(const HeaderMap& header_map) {
  if (!header_map.ForwardedFor()) {
    return "";
  }
  std::vector<std::string> xff_address_list =
      StringUtil::split(header_map.ForwardedFor()->value().c_str(), ',');
  if (xff_address_list.empty()) {
    return "";
  }
  return xff_address_list.front();
}

std::string GetLastForwardedHost(const HeaderMap& header_map) {
  const HeaderEntry* entry = header_map.get(kHeaderNameXFH);
  if (entry == nullptr) {
    return "";
  }
  auto xff_list = StringUtil::split(entry->value().c_str(), ',');
  if (xff_list.empty()) {
    return "";
  }
  return xff_list.back();
}

void FillRequestHeaderAttributes(const HeaderMap& header_map,
                                 Attributes* attr) {
  // Pass in all headers
  header_map.iterate(
      [](const HeaderEntry& header, void* context) {
        auto attr = static_cast<Attributes*>(context);
        attr->attributes[kRequestHeaderPrefix + header.key().c_str()] =
            StringValue(header.value().c_str());
      },
      attr);

  SetStringAttribute(kAttrNameRequestPath, header_map.Path()->value().c_str(),
                     attr);
  SetStringAttribute(kAttrNameOriginIp, GetFirstForwardedFor(header_map), attr);
  SetStringAttribute(kAttrNameOriginHost, GetLastForwardedHost(header_map),
                     attr);
}

void FillResponseHeaderAttributes(const HeaderMap& header_map,
                                  Attributes* attr) {
  header_map.iterate(
      [](const HeaderEntry& header, void* context) {
        auto attr = static_cast<Attributes*>(context);
        attr->attributes[kResponseHeaderPrefix + header.key().c_str()] =
            StringValue(header.value().c_str());
      },
      attr);
}

void FillRequestInfoAttributes(const AccessLog::RequestInfo& info,
                               int check_status_code, Attributes* attr) {
  if (info.bytesReceived() >= 0) {
    attr->attributes[kAttrNameRequestSize] = Int64Value(info.bytesReceived());
  }
  if (info.bytesSent() >= 0) {
    attr->attributes[kAttrNameResponseSize] = Int64Value(info.bytesSent());
  }
  if (info.duration().count() >= 0) {
    attr->attributes[kAttrNameResponseTime] =
        Int64Value(info.duration().count());
  }
  if (info.responseCode().valid()) {
    attr->attributes[kResponseStatusCode] =
        StringValue(std::to_string(info.responseCode().value()));
  } else {
    attr->attributes[kResponseStatusCode] =
        StringValue(std::to_string(check_status_code));
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

void HttpControl::FillCheckAttributes(const HeaderMap& header_map,
                                      Attributes* attr) {
  FillRequestHeaderAttributes(header_map, attr);

  for (const auto& attribute : config_attributes_) {
    SetStringAttribute(attribute.first, attribute.second, attr);
  }
}

void HttpControl::Check(HttpRequestDataPtr request_data, HeaderMap& headers,
                        DoneFunc on_done) {
  FillCheckAttributes(headers, &request_data->attributes);
  log().debug("Send Check: {}", request_data->attributes.DebugString());
  mixer_client_->Check(request_data->attributes, on_done);
}

void HttpControl::Report(HttpRequestDataPtr request_data,
                         const HeaderMap* response_headers,
                         const AccessLog::RequestInfo& request_info,
                         int check_status, DoneFunc on_done) {
  // Use all Check attributes for Report.
  // Add additional Report attributes.
  FillResponseHeaderAttributes(*response_headers, &request_data->attributes);
  FillRequestInfoAttributes(request_info, check_status,
                            &request_data->attributes);
  log().debug("Send Report: {}", request_data->attributes.DebugString());
  mixer_client_->Report(request_data->attributes, on_done);
}

}  // namespace Mixer
}  // namespace Http
