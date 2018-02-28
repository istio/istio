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
  const HeaderMap* headers_;
  const RequestInfo::RequestInfo& info_;

 public:
  ReportData(const HeaderMap* headers, const RequestInfo::RequestInfo& info)
      : headers_(headers), info_(info) {}

  std::map<std::string, std::string> GetResponseHeaders() const override {
    if (headers_) {
      return Utils::ExtractHeaders(*headers_, ResponseHeaderExclusives);
    }
    return std::map<std::string, std::string>();
  }

  void GetReportInfo(
      ::istio::control::http::ReportData::ReportInfo* data) const override {
    data->received_bytes = info_.bytesReceived();
    data->send_bytes = info_.bytesSent();
    data->duration =
        std::chrono::duration_cast<std::chrono::nanoseconds>(info_.duration());
    // responseCode is for the backend response. If it is not valid, the request
    // is rejected by Envoy. Set the response code for such requests as 500.
    data->response_code = 500;
    if (info_.responseCode().valid()) {
      data->response_code = info_.responseCode().value();
    }
  }
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
