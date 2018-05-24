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

void AttributesBuilder::ExtractAuthAttributes(CheckData *check_data) {
  istio::authn::Result authn_result;
  if (check_data->GetAuthenticationResult(&authn_result)) {
    utils::AttributesBuilder builder(&request_->attributes);
    if (!authn_result.principal().empty()) {
      builder.AddString(AttributeName::kRequestAuthPrincipal,
                        authn_result.principal());
    }
    if (!authn_result.peer_user().empty()) {
      // TODO(diemtvu): remove kSourceUser once migration to source.principal is
      // over. https://github.com/istio/istio/issues/4689
      builder.AddString(AttributeName::kSourceUser, authn_result.peer_user());
      builder.AddString(AttributeName::kSourcePrincipal,
                        authn_result.peer_user());
    }
    if (authn_result.has_origin()) {
      const auto &origin = authn_result.origin();
      if (!origin.audiences().empty()) {
        // TODO(diemtvu): this should be send as repeated field once mixer
        // support string_list (https://github.com/istio/istio/issues/2802) For
        // now, just use the first value.
        builder.AddString(AttributeName::kRequestAuthAudiences,
                          origin.audiences(0));
      }
      if (!origin.presenter().empty()) {
        builder.AddString(AttributeName::kRequestAuthPresenter,
                          origin.presenter());
      }
      if (!origin.claims().empty()) {
        builder.AddProtobufStringMap(AttributeName::kRequestAuthClaims,
                                     origin.claims());
      }
      if (!origin.raw_claims().empty()) {
        builder.AddString(AttributeName::kRequestAuthRawClaims,
                          origin.raw_claims());
      }
    }
    return;
  }

  // Fallback to extract from jwt filter directly. This can be removed once
  // authn filter is in place.
  std::map<std::string, std::string> payload;
  utils::AttributesBuilder builder(&request_->attributes);
  if (check_data->GetJWTPayload(&payload) && !payload.empty()) {
    // Populate auth attributes.
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
  std::string source_user;
  if (check_data->GetSourceUser(&source_user)) {
    // TODO(diemtvu): remove kSourceUser once migration to source.principal is
    // over. https://github.com/istio/istio/issues/4689
    builder.AddString(AttributeName::kSourceUser, source_user);
    builder.AddString(AttributeName::kSourcePrincipal, source_user);
  }
}  // namespace http

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
  ExtractAuthAttributes(check_data);

  utils::AttributesBuilder builder(&request_->attributes);

  builder.AddBool(AttributeName::kConnectionMtls, check_data->IsMutualTLS());

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

  std::string dest_ip;
  int dest_port;
  // Do not overwrite destination IP and port if it has already been set.
  if (report_data->GetDestinationIpPort(&dest_ip, &dest_port)) {
    if (!builder.HasAttribute(AttributeName::kDestinationIp)) {
      builder.AddBytes(AttributeName::kDestinationIp, dest_ip);
    }
    if (!builder.HasAttribute(AttributeName::kDestinationPort)) {
      builder.AddInt64(AttributeName::kDestinationPort, dest_port);
    }
  }

  std::string uid;
  if (report_data->GetDestinationUID(&uid)) {
    builder.AddString(AttributeName::kDestinationUID, uid);
  }

  std::map<std::string, std::string> headers =
      report_data->GetResponseHeaders();
  builder.AddStringMap(AttributeName::kResponseHeaders, headers);

  builder.AddTimestamp(AttributeName::kResponseTime,
                       std::chrono::system_clock::now());

  ReportData::ReportInfo info;
  report_data->GetReportInfo(&info);
  builder.AddInt64(AttributeName::kRequestBodySize, info.request_body_size);
  builder.AddInt64(AttributeName::kResponseBodySize, info.response_body_size);
  builder.AddInt64(AttributeName::kRequestTotalSize, info.request_total_size);
  builder.AddInt64(AttributeName::kResponseTotalSize, info.response_total_size);
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
