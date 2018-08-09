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

#ifndef ISTIO_CONTROL_HTTP_REPORT_DATA_H
#define ISTIO_CONTROL_HTTP_REPORT_DATA_H

#include <chrono>
#include <map>

namespace istio {
namespace control {
namespace http {

// The interface to extract HTTP data for Mixer report.
// Implemented by the environment (Envoy) and used by the library.
class ReportData {
 public:
  virtual ~ReportData() {}

  // Get response HTTP headers.
  virtual std::map<std::string, std::string> GetResponseHeaders() const = 0;

  // Get additional report info.
  struct ReportInfo {
    uint64_t response_total_size;
    uint64_t request_total_size;
    uint64_t request_body_size;
    uint64_t response_body_size;
    std::chrono::nanoseconds duration;
    int response_code;
    std::string response_flags;
  };
  virtual void GetReportInfo(ReportInfo* info) const = 0;

  // Get destination ip/port.
  virtual bool GetDestinationIpPort(std::string* ip, int* port) const = 0;

  // Get Rbac attributes.
  struct RbacReportInfo {
    std::string permissive_resp_code;
    std::string permissive_policy_id;
  };
  virtual bool GetRbacReportInfo(RbacReportInfo* report_info) const = 0;

  // Get upstream host UID. This value overrides the value in the report bag.
  virtual bool GetDestinationUID(std::string* uid) const = 0;

  // gRPC status info
  struct GrpcStatus {
    std::string status;
    std::string message;
  };
  virtual bool GetGrpcStatus(GrpcStatus* status) const = 0;
};

}  // namespace http
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_HTTP_REPORT_DATA_H
