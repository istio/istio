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
using ::istio::mixer_client::CheckOptions;
using ::istio::mixer_client::Attributes;
using ::istio::mixer_client::DoneFunc;
using ::istio::mixer_client::MixerClientOptions;
using ::istio::mixer_client::QuotaOptions;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// Define attribute names
const std::string kOriginUser = "origin.user";

const std::string kRequestHeaders = "request.headers";
const std::string kRequestHost = "request.host";
const std::string kRequestMethod = "request.method";
const std::string kRequestPath = "request.path";
const std::string kRequestReferer = "request.referer";
const std::string kRequestScheme = "request.scheme";
const std::string kRequestSize = "request.size";
const std::string kRequestTime = "request.time";
const std::string kRequestUserAgent = "request.useragent";

const std::string kResponseCode = "response.code";
const std::string kResponseDuration = "response.duration";
const std::string kResponseHeaders = "response.headers";
const std::string kResponseSize = "response.size";
const std::string kResponseTime = "response.time";

// Keys to well-known headers
const LowerCaseString kRefererHeaderKey("referer");

// Check cache size: 10000 cache entries.
const int kCheckCacheEntries = 10000;
// Default check cache expired in 5 minutes.
const int kCheckCacheExpirationInSeconds = 300;

CheckOptions GetCheckOptions(const MixerConfig& config) {
  int expiration = kCheckCacheExpirationInSeconds;
  if (!config.check_cache_expiration.empty()) {
    expiration = std::stoi(config.check_cache_expiration);
  }

  // Remove expired items from cache 1 second later.
  CheckOptions options(kCheckCacheEntries, expiration * 1000,
                       (expiration + 1) * 1000);

  options.cache_keys = config.check_cache_keys;

  if (config.network_fail_policy == "close") {
    options.network_fail_open = false;
  }

  return options;
}

QuotaOptions GetQuotaOptions(const MixerConfig& config) {
  if (config.quota_cache == "on") {
    return QuotaOptions();
  } else {
    // Use num_entries=0 to disable cache.
    // the 2nd parameter is not used in the disable case.
    return QuotaOptions(0, 1000);
  }
}

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

  // Since we're in an HTTP filter, if the scheme header doesn't exist we can
  // fill it in with a reasonable value.
  SetStringAttribute(
      kRequestScheme,
      header_map.Scheme() ? header_map.Scheme()->value().c_str() : "http",
      attr);

  if (header_map.UserAgent()) {
    SetStringAttribute(kRequestUserAgent,
                       header_map.UserAgent()->value().c_str(), attr);
  }
  if (header_map.Method()) {
    SetStringAttribute(kRequestMethod, header_map.Method()->value().c_str(),
                       attr);
  }

  const HeaderEntry* referer = header_map.get(kRefererHeaderKey);
  if (referer) {
    std::string val(referer->value().c_str(), referer->value().size());
    SetStringAttribute(kRequestReferer, val, attr);
  }

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

  attr->attributes[kResponseDuration] = Attributes::DurationValue(
      std::chrono::duration_cast<std::chrono::nanoseconds>(info.duration()));

  if (info.responseCode().valid()) {
    attr->attributes[kResponseCode] =
        Attributes::Int64Value(info.responseCode().value());
  } else {
    attr->attributes[kResponseCode] = Attributes::Int64Value(check_status_code);
  }
}

}  // namespace

HttpControl::HttpControl(const MixerConfig& mixer_config)
    : mixer_config_(mixer_config) {
  MixerClientOptions options(GetCheckOptions(mixer_config),
                             GetQuotaOptions(mixer_config));
  options.mixer_server = mixer_config_.mixer_server;
  mixer_client_ = ::istio::mixer_client::CreateMixerClient(options);

  mixer_config_.ExtractQuotaAttributes(&quota_attributes_);
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

  for (const auto& attribute : mixer_config_.mixer_attributes) {
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

void HttpControl::Quota(HttpRequestDataPtr request_data, DoneFunc on_done) {
  if (quota_attributes_.attributes.empty()) {
    on_done(Status::OK);
    return;
  }

  for (const auto& attribute : quota_attributes_.attributes) {
    request_data->attributes.attributes[attribute.first] = attribute.second;
  }
  // quota() needs all the attributes that check() needs
  // operator may apply conditional quota using attributes in selectors.
  // mixerClient::GenerateSignature should be updated
  // to exclude non identifying attributes
  log().debug("Send Quota: {}", request_data->attributes.DebugString());
  mixer_client_->Quota(request_data->attributes, on_done);
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
}  // namespace Envoy
