/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#include "proxy/src/istio/control/http/attributes_builder.h"

#include <set>

#include "proxy/include/istio/utils/attribute_names.h"
#include "proxy/include/istio/utils/attributes_builder.h"
#include "proxy/include/istio/utils/status.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::Attributes_StringMap;

namespace istio {
namespace control {
namespace http {
namespace {
// The gRPC content types.
const std::set<std::string> kGrpcContentTypes{
    "application/grpc", "application/grpc+proto", "application/grpc+json"};

}  // namespace

void AttributesBuilder::ExtractRequestHeaderAttributes(CheckData *check_data) {
  utils::AttributesBuilder builder(&request_->attributes);
  std::map<std::string, std::string> headers = check_data->GetRequestHeaders();
  builder.AddStringMap(utils::AttributeName::kRequestHeaders, headers);

  struct TopLevelAttr {
    CheckData::HeaderType header_type;
    const std::string &name;
    bool set_default;
    const char *default_value;
  };
  static TopLevelAttr attrs[] = {
      {CheckData::HEADER_HOST, utils::AttributeName::kRequestHost, true, ""},
      {CheckData::HEADER_METHOD, utils::AttributeName::kRequestMethod, false,
       ""},
      {CheckData::HEADER_PATH, utils::AttributeName::kRequestPath, true, ""},
      {CheckData::HEADER_REFERER, utils::AttributeName::kRequestReferer, false,
       ""},
      {CheckData::HEADER_SCHEME, utils::AttributeName::kRequestScheme, true,
       "http"},
      {CheckData::HEADER_USER_AGENT, utils::AttributeName::kRequestUserAgent,
       false, ""},
  };

  for (const auto &it : attrs) {
    std::string data;
    if (check_data->FindHeaderByType(it.header_type, &data)) {
      builder.AddString(it.name, data);
    } else if (it.set_default) {
      builder.AddString(it.name, it.default_value);
    }
  }

  std::string query_path;
  if (check_data->GetUrlPath(&query_path)) {
    builder.AddString(utils::AttributeName::kRequestUrlPath, query_path);
  }

  std::map<std::string, std::string> query_map;
  if (check_data->GetRequestQueryParams(&query_map) && query_map.size() > 0) {
    builder.AddStringMap(utils::AttributeName::kRequestQueryParams, query_map);
  }
}

void AttributesBuilder::ExtractAuthAttributes(CheckData *check_data) {
  utils::AttributesBuilder builder(&request_->attributes);

  std::string destination_principal;
  if (check_data->GetPrincipal(false, &destination_principal)) {
    builder.AddString(utils::AttributeName::kDestinationPrincipal,
                      destination_principal);
  }
  static const std::set<std::string> kAuthenticationStringAttributes = {
      utils::AttributeName::kRequestAuthPrincipal,
      utils::AttributeName::kSourceUser,
      utils::AttributeName::kSourcePrincipal,
      utils::AttributeName::kRequestAuthAudiences,
      utils::AttributeName::kRequestAuthPresenter,
      utils::AttributeName::kRequestAuthRawClaims,
  };
  const auto *authn_result = check_data->GetAuthenticationResult();
  if (authn_result != nullptr) {
    // Not all data in authentication results need to be sent to mixer (e.g
    // groups), so we need to iterate on pre-approved attributes only.
    for (const auto &attribute : kAuthenticationStringAttributes) {
      const auto &iter = authn_result->fields().find(attribute);
      if (iter != authn_result->fields().end() &&
          !iter->second.string_value().empty()) {
        builder.AddString(attribute, iter->second.string_value());
      }
    }

    // Add string-map attribute (kRequestAuthClaims)
    const auto &claims =
        authn_result->fields().find(utils::AttributeName::kRequestAuthClaims);
    if (claims != authn_result->fields().end()) {
      builder.AddProtoStructStringMap(utils::AttributeName::kRequestAuthClaims,
                                      claims->second.struct_value());
    }
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
  ExtractAuthAttributes(check_data);

  utils::AttributesBuilder builder(&request_->attributes);

  // connection remote IP is always reported as origin IP
  std::string source_ip;
  int source_port;
  if (check_data->GetSourceIpPort(&source_ip, &source_port)) {
    builder.AddBytes(utils::AttributeName::kOriginIp, source_ip);
  }

  builder.AddBool(utils::AttributeName::kConnectionMtls,
                  check_data->IsMutualTLS());

  std::string requested_server_name;
  if (check_data->GetRequestedServerName(&requested_server_name)) {
    builder.AddString(utils::AttributeName::kConnectionRequestedServerName,
                      requested_server_name);
  }

  builder.AddTimestamp(utils::AttributeName::kRequestTime,
                       std::chrono::system_clock::now());

  std::string protocol = "http";
  std::string content_type;
  if (check_data->FindHeaderByType(CheckData::HEADER_CONTENT_TYPE,
                                   &content_type)) {
    if (kGrpcContentTypes.count(content_type) != 0) {
      protocol = "grpc";
    }
  }
  builder.AddString(utils::AttributeName::kContextProtocol, protocol);
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
    if (!builder.HasAttribute(utils::AttributeName::kDestinationIp)) {
      builder.AddBytes(utils::AttributeName::kDestinationIp, dest_ip);
    }
    if (!builder.HasAttribute(utils::AttributeName::kDestinationPort)) {
      builder.AddInt64(utils::AttributeName::kDestinationPort, dest_port);
    }
  }

  std::string uid;
  if (report_data->GetDestinationUID(&uid)) {
    builder.AddString(utils::AttributeName::kDestinationUID, uid);
  }

  std::map<std::string, std::string> headers =
      report_data->GetResponseHeaders();
  builder.AddStringMap(utils::AttributeName::kResponseHeaders, headers);

  builder.AddTimestamp(utils::AttributeName::kResponseTime,
                       std::chrono::system_clock::now());

  ReportData::ReportInfo info;
  report_data->GetReportInfo(&info);
  builder.AddInt64(utils::AttributeName::kRequestBodySize,
                   info.request_body_size);
  builder.AddInt64(utils::AttributeName::kResponseBodySize,
                   info.response_body_size);
  builder.AddInt64(utils::AttributeName::kRequestTotalSize,
                   info.request_total_size);
  builder.AddInt64(utils::AttributeName::kResponseTotalSize,
                   info.response_total_size);
  builder.AddDuration(utils::AttributeName::kResponseDuration, info.duration);
  if (!request_->check_status.ok()) {
    builder.AddInt64(
        utils::AttributeName::kResponseCode,
        utils::StatusHttpCode(request_->check_status.error_code()));
    builder.AddInt64(utils::AttributeName::kCheckErrorCode,
                     request_->check_status.error_code());
    builder.AddString(utils::AttributeName::kCheckErrorMessage,
                      request_->check_status.ToString());
  } else {
    builder.AddInt64(utils::AttributeName::kResponseCode, info.response_code);
  }

  ReportData::GrpcStatus grpc_status;
  if (report_data->GetGrpcStatus(&grpc_status)) {
    builder.AddString(utils::AttributeName::kResponseGrpcStatus,
                      grpc_status.status);
    builder.AddString(utils::AttributeName::kResponseGrpcMessage,
                      grpc_status.message);
  }

  builder.AddString(utils::AttributeName::kContextProxyErrorCode,
                    info.response_flags);

  ReportData::RbacReportInfo rbac_info;
  if (report_data->GetRbacReportInfo(&rbac_info)) {
    if (!rbac_info.permissive_resp_code.empty()) {
      builder.AddString(utils::AttributeName::kRbacPermissiveResponseCode,
                        rbac_info.permissive_resp_code);
    }
    if (!rbac_info.permissive_policy_id.empty()) {
      builder.AddString(utils::AttributeName::kRbacPermissivePolicyId,
                        rbac_info.permissive_policy_id);
    }
  }
}

}  // namespace http
}  // namespace control
}  // namespace istio
