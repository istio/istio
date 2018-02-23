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

#include "src/istio/control/http/attributes_builder.h"

#include "include/istio/utils/attributes_builder.h"
#include "include/istio/utils/status.h"
#include "src/istio/control/attribute_names.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::Attributes_StringMap;

namespace istio {
namespace control {
namespace http {

void AttributesBuilder::ExtractRequestHeaderAttributes(CheckData *check_data) {
  utils::AttributesBuilder builder(&request_->attributes);
  std::map<std::string, std::string> headers = check_data->GetRequestHeaders();
  builder.AddStringMap(AttributeName::kRequestHeaders, headers);

  struct TopLevelAttr {
    CheckData::HeaderType header_type;
    const std::string &name;
    bool set_default;
    const char *default_value;
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

  for (const auto &it : attrs) {
    std::string data;
    if (check_data->FindHeaderByType(it.header_type, &data)) {
      builder.AddString(it.name, data);
    } else if (it.set_default) {
      builder.AddString(it.name, it.default_value);
    }
  }
}

void AttributesBuilder::ExtractRequestAuthAttributes(CheckData *check_data) {
  std::map<std::string, std::string> payload;
  if (check_data->GetJWTPayload(&payload) && !payload.empty()) {
    // Populate auth attributes.
    utils::AttributesBuilder builder(&request_->attributes);
    if (payload.count("iss") > 0 && payload.count("sub") > 0) {
      builder.AddString(AttributeName::kRequestAuthPrincipal,
                        payload["iss"] + "/" + payload["sub"]);
    }
    if (payload.count("aud") > 0) {
      builder.AddString(AttributeName::kRequestAuthAudiences, payload["aud"]);
    }
    if (payload.count("azp") > 0) {
      builder.AddString(AttributeName::kRequestAuthPresenter, payload["azp"]);
    }
    builder.AddStringMap(AttributeName::kRequestAuthClaims, payload);
  }
}

void AttributesBuilder::ExtractForwardedAttributes(CheckData *check_data) {
  std::string forwarded_data;
  if (!check_data->ExtractIstioAttributes(&forwarded_data)) {
    return;
  }
  Attributes v2_format;
  if (v2_format.ParseFromString(forwarded_data)) {
    request_->attributes.MergeFrom(v2_format);
    return;
  }
}

void AttributesBuilder::ExtractCheckAttributes(CheckData *check_data) {
  ExtractRequestHeaderAttributes(check_data);
  ExtractRequestAuthAttributes(check_data);

  utils::AttributesBuilder builder(&request_->attributes);

  std::string source_ip;
  int source_port;
  if (check_data->GetSourceIpPort(&source_ip, &source_port)) {
    builder.AddBytes(AttributeName::kSourceIp, source_ip);
    builder.AddInt64(AttributeName::kSourcePort, source_port);
  }
  builder.AddBool(AttributeName::kConnectionMtls, check_data->IsMutualTLS());

  std::string source_user;
  if (check_data->GetSourceUser(&source_user)) {
    builder.AddString(AttributeName::kSourceUser, source_user);
  }
  builder.AddTimestamp(AttributeName::kRequestTime,
                       std::chrono::system_clock::now());
  builder.AddString(AttributeName::kContextProtocol, "http");
}

void AttributesBuilder::ForwardAttributes(const Attributes &forward_attributes,
                                          HeaderUpdate *header_update) {
  std::string str;
  forward_attributes.SerializeToString(&str);
  header_update->AddIstioAttributes(str);
}

void AttributesBuilder::ExtractReportAttributes(ReportData *report_data) {
  utils::AttributesBuilder builder(&request_->attributes);

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
    builder.AddInt64(AttributeName::kCheckErrorCode,
                     request_->check_status.error_code());
    builder.AddString(AttributeName::kCheckErrorMessage,
                      request_->check_status.ToString());
  } else {
    builder.AddInt64(AttributeName::kResponseCode, info.response_code);
  }
}

}  // namespace http
}  // namespace control
}  // namespace istio
