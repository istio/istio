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

#pragma once

#include "common/request_info/utility.h"
#include "envoy/http/header_map.h"
#include "envoy/request_info/request_info.h"
#include "extensions/filters/http/well_known_names.h"
#include "include/istio/control/http/controller.h"
#include "src/envoy/utils/utils.h"

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {
const std::string kRbacPermissivePolicyIDField = "shadow_effective_policyID";
const std::string kRbacPermissiveRespCodeField = "shadow_response_code";

// Set of headers excluded from response.headers attribute.
const std::set<std::string> ResponseHeaderExclusives = {};

bool ExtractGrpcStatus(const HeaderMap *headers,
                       ::istio::control::http::ReportData::GrpcStatus *status) {
  if (headers != nullptr && headers->GrpcStatus()) {
    status->status = std::string(headers->GrpcStatus()->value().c_str(),
                                 headers->GrpcStatus()->value().size());
    if (headers->GrpcMessage()) {
      status->message = std::string(headers->GrpcMessage()->value().c_str(),
                                    headers->GrpcMessage()->value().size());
    }
    return true;
  }
  return false;
}

}  // namespace

class ReportData : public ::istio::control::http::ReportData {
  const HeaderMap *headers_;
  const HeaderMap *trailers_;
  const RequestInfo::RequestInfo &info_;
  uint64_t response_total_size_;
  uint64_t request_total_size_;

 public:
  ReportData(const HeaderMap *headers, const HeaderMap *response_trailers,
             const RequestInfo::RequestInfo &info, uint64_t request_total_size)
      : headers_(headers),
        trailers_(response_trailers),
        info_(info),
        response_total_size_(info.bytesSent()),
        request_total_size_(request_total_size) {
    if (headers != nullptr) {
      response_total_size_ += headers->byteSize();
    }
    if (response_trailers != nullptr) {
      response_total_size_ += response_trailers->byteSize();
    }
  }

  std::map<std::string, std::string> GetResponseHeaders() const override {
    std::map<std::string, std::string> header_map;
    if (headers_) {
      Utils::ExtractHeaders(*headers_, ResponseHeaderExclusives, header_map);
    }
    if (trailers_) {
      Utils::ExtractHeaders(*trailers_, ResponseHeaderExclusives, header_map);
    }
    return header_map;
  }

  void GetReportInfo(
      ::istio::control::http::ReportData::ReportInfo *data) const override {
    data->request_body_size = info_.bytesReceived();
    data->response_body_size = info_.bytesSent();
    data->response_total_size = response_total_size_;
    data->request_total_size = request_total_size_;
    data->duration =
        info_.requestComplete().value_or(std::chrono::nanoseconds{0});
    // responseCode is for the backend response. If it is not valid, the request
    // is rejected by Envoy. Set the response code for such requests as 500.
    data->response_code = info_.responseCode().value_or(500);

    data->response_flags = RequestInfo::ResponseFlagUtils::toShortString(info_);
  }

  bool GetDestinationIpPort(std::string *str_ip, int *port) const override {
    if (info_.upstreamHost() && info_.upstreamHost()->address()) {
      return Utils::GetIpPort(info_.upstreamHost()->address()->ip(), str_ip,
                              port);
    }
    return false;
  }

  bool GetDestinationUID(std::string *uid) const override {
    if (info_.upstreamHost()) {
      return Utils::GetDestinationUID(*info_.upstreamHost()->metadata(), uid);
    }
    return false;
  }

  bool GetGrpcStatus(GrpcStatus *status) const override {
    // Check trailer first.
    // If not response body, grpc-status is in response headers.
    return ExtractGrpcStatus(trailers_, status) ||
           ExtractGrpcStatus(headers_, status);
  }

  // Get Rbac related attributes.
  bool GetRbacReportInfo(RbacReportInfo *report_info) const override {
    const auto filter_meta = info_.dynamicMetadata().filter_metadata();
    const auto filter_it =
        filter_meta.find(Extensions::HttpFilters::HttpFilterNames::get().Rbac);
    if (filter_it == filter_meta.end()) {
      return false;
    }

    const auto &data_struct = filter_it->second;
    const auto resp_code_it =
        data_struct.fields().find(kRbacPermissiveRespCodeField);
    if (resp_code_it != data_struct.fields().end()) {
      report_info->permissive_resp_code = resp_code_it->second.string_value();
    }

    const auto policy_id_it =
        data_struct.fields().find(kRbacPermissivePolicyIDField);
    if (policy_id_it != data_struct.fields().end()) {
      report_info->permissive_policy_id = policy_id_it->second.string_value();
    }

    return !report_info->permissive_resp_code.empty() ||
           !report_info->permissive_policy_id.empty();
  }
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
