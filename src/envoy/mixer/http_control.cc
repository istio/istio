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

const std::string kProxyPeerID = "Istio/Proxy";

// Define lower case string for X-Forwarded-Host.
const LowerCaseString kHeaderNameXFH("x-forwarded-host", false);

const std::string kRequestHeaderPrefix = "requestHeader:";
const std::string kRequestParamPrefix = "requestParameter:";
const std::string kResponseHeaderPrefix = "responseHeader:";

// Define attribute names
const std::string kAttrNamePeerId = "peerId";
const std::string kAttrNameURL = "url";
const std::string kAttrNameHttpMethod = "httpMethod";
const std::string kAttrNameRequestSize = "requestSize";
const std::string kAttrNameResponseSize = "responseSize";
const std::string kAttrNameLogMessage = "logMessage";
const std::string kAttrNameResponseTime = "responseTime";
const std::string kAttrNameOriginIp = "originIp";
const std::string kAttrNameOriginHost = "originHost";

const std::string kEnvNameSourceService = "SOURCE_SERVICE";
const std::string kEnvNameTargetService = "TARGET_SERVICE";

const std::string kAttrNameSourceService = "sourceService";
const std::string kAttrNameTargetService = "targetService";

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

  // Pass in all Query parameters.
  auto path = header_map.Path();
  if (path != nullptr) {
    for (const auto& it : Utility::parseQueryString(path->value().c_str())) {
      attr->attributes[kRequestParamPrefix + it.first] = StringValue(it.second);
    }
  }

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
                               Attributes* attr) {
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
}

}  // namespace

HttpControl::HttpControl(const std::string& mixer_server) {
  ::istio::mixer_client::MixerClientOptions options;
  options.mixer_server = mixer_server;
  mixer_client_ = ::istio::mixer_client::CreateMixerClient(options);

  auto source_service = getenv(kEnvNameSourceService.c_str());
  if (source_service) {
    source_service_ = source_service;
  }
  auto target_service = getenv(kEnvNameTargetService.c_str());
  if (target_service) {
    target_service_ = target_service;
  }
}

void HttpControl::FillCheckAttributes(const HeaderMap& header_map,
                                      Attributes* attr) {
  FillRequestHeaderAttributes(header_map, attr);

  SetStringAttribute(kAttrNameSourceService, source_service_, attr);
  SetStringAttribute(kAttrNameTargetService, target_service_, attr);
  attr->attributes[kAttrNamePeerId] = StringValue(kProxyPeerID);
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
                         DoneFunc on_done) {
  // Use all Check attributes for Report.
  // Add additional Report attributes.
  FillResponseHeaderAttributes(*response_headers, &request_data->attributes);
  FillRequestInfoAttributes(request_info, &request_data->attributes);
  log().debug("Send Report: {}", request_data->attributes.DebugString());
  mixer_client_->Report(request_data->attributes, on_done);
}

}  // namespace Mixer
}  // namespace Http
