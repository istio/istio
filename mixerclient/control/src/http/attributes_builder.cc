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

#include "attributes_builder.h"

#include "control/include/utils/status.h"
#include "control/src/attribute_names.h"
#include "include/attributes_builder.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::Attributes_StringMap;

namespace istio {
namespace mixer_control {
namespace http {

void AttributesBuilder::ExtractRequestHeaderAttributes(CheckData* check_data) {
  ::istio::mixer_client::AttributesBuilder builder(&request_->attributes);
  std::map<std::string, std::string> headers = check_data->GetRequestHeaders();
  builder.AddStringMap(AttributeName::kRequestHeaders, headers);

  struct TopLevelAttr {
    CheckData::HeaderType header_type;
    const std::string& name;
    bool set_default;
    const char* default_value;
  };
  static TopLevelAttr attrs[] = {
      {CheckData::HEADER_HOST, AttributeName::kRequestHost, true, ""},
      {CheckData::HEADER_METHOD, AttributeName::kRequestMethod, false, ""},
      {CheckData::HEADER_PATH, AttributeName::kRequestPath, true, ""},
      {CheckData::HEADER_REFERER, AttributeName::kRequestReferer, false, ""},
      {CheckData::HEADER_SCHEME, AttributeName::kRequestScheme, true, "http"},
      {CheckData::HEADER_USER_AGENT, AttributeName::kRequestUserAgent, false,
       ""},
  };

  for (const auto& it : attrs) {
    std::string data;
    if (check_data->FindRequestHeader(it.header_type, &data)) {
      builder.AddString(it.name, data);
    } else if (it.set_default) {
      builder.AddString(it.name, it.default_value);
    }
  }
}

void AttributesBuilder::ExtractForwardedAttributes(CheckData* check_data) {
  std::string forwarded_data;
  if (!check_data->ExtractIstioAttributes(&forwarded_data)) {
    return;
  }
  Attributes v2_format;
  if (v2_format.ParseFromString(forwarded_data)) {
    request_->attributes.MergeFrom(v2_format);
    return;
  }

  // Legacy format from old proxy.
  Attributes_StringMap forwarded_attributes;
  if (forwarded_attributes.ParseFromString(forwarded_data)) {
    ::istio::mixer_client::AttributesBuilder builder(&request_->attributes);
    for (const auto& it : forwarded_attributes.entries()) {
      builder.AddIpOrString(it.first, it.second);
    }
  }
}

void AttributesBuilder::ExtractCheckAttributes(CheckData* check_data) {
  ExtractRequestHeaderAttributes(check_data);

  ::istio::mixer_client::AttributesBuilder builder(&request_->attributes);

  std::string source_ip;
  int source_port;
  if (check_data->GetSourceIpPort(&source_ip, &source_port)) {
    builder.AddBytes(AttributeName::kSourceIp, source_ip);
    builder.AddInt64(AttributeName::kSourcePort, source_port);
  }

  std::string source_user;
  if (check_data->GetSourceUser(&source_user)) {
    builder.AddString(AttributeName::kSourceUser, source_user);
  }
  builder.AddTimestamp(AttributeName::kRequestTime,
                       std::chrono::system_clock::now());
  builder.AddString(AttributeName::kContextProtocol, "http");
}

void AttributesBuilder::ForwardAttributes(const Attributes& forward_attributes,
                                          CheckData* check_data) {
  std::string str;
  forward_attributes.SerializeToString(&str);
  check_data->AddIstioAttributes(str);
}

void AttributesBuilder::ExtractReportAttributes(ReportData* report_data) {
  ::istio::mixer_client::AttributesBuilder builder(&request_->attributes);
  std::map<std::string, std::string> headers =
      report_data->GetResponseHeaders();
  builder.AddStringMap(AttributeName::kResponseHeaders, headers);

  builder.AddTimestamp(AttributeName::kResponseTime,
                       std::chrono::system_clock::now());

  ReportData::ReportInfo info;
  report_data->GetReportInfo(&info);
  builder.AddInt64(AttributeName::kRequestSize, info.received_bytes);
  builder.AddInt64(AttributeName::kResponseSize, info.send_bytes);
  builder.AddDuration(AttributeName::kResponseDuration, info.duration);
  if (!request_->check_status.ok()) {
    builder.AddInt64(
        AttributeName::kResponseCode,
        utils::StatusHttpCode(request_->check_status.error_code()));
  } else {
    builder.AddInt64(AttributeName::kResponseCode, info.response_code);
  }
}

}  // namespace http
}  // namespace mixer_control
}  // namespace istio
