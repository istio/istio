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

#include "envoy/http/header_map.h"
#include "envoy/request_info/request_info.h"
#include "include/istio/control/http/controller.h"
#include "src/envoy/utils/utils.h"

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {
// Set of headers excluded from response.headers attribute.
const std::set<std::string> ResponseHeaderExclusives = {};

}  // namespace

class ReportData : public ::istio::control::http::ReportData {
  const HeaderMap *headers_;
  const RequestInfo::RequestInfo &info_;
  uint64_t response_total_size_;
  uint64_t request_total_size_;

 public:
  ReportData(const HeaderMap *headers, const HeaderMap *response_trailers,
             const RequestInfo::RequestInfo &info, uint64_t request_total_size)
      : headers_(headers),
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
    if (headers_) {
      return Utils::ExtractHeaders(*headers_, ResponseHeaderExclusives);
    }
    return std::map<std::string, std::string>();
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
      return Utils::GetDestinationUID(info_.upstreamHost()->metadata(), uid);
    }
    return false;
  }
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
