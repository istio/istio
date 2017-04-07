/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_SERVICE_CONTROL_PROTO_H_
#define API_MANAGER_SERVICE_CONTROL_PROTO_H_

#include "contrib/endpoints/include/api_manager/utils/status.h"
#include "contrib/endpoints/src/api_manager/service_control/info.h"
#include "google/api/label.pb.h"
#include "google/api/metric.pb.h"
#include "google/api/servicecontrol/v1/quota_controller.pb.h"
#include "google/api/servicecontrol/v1/service_controller.pb.h"

namespace google {
namespace api_manager {
namespace service_control {

class Proto final {
 public:
  // Initializes Proto with all supported metrics and labels.
  Proto(const std::set<std::string>& logs, const std::string& service_name,
        const std::string& service_config_id);

  // Initializes Proto with specified (and supported) metrics and
  // labels.
  Proto(const std::set<std::string>& logs, const std::set<std::string>& metrics,
        const std::set<std::string>& labels, const std::string& service_name,
        const std::string& service_config_id);

  // Fills the CheckRequest protobuf from info.
  // There are some logic inside the Fill functions beside just filling
  // the fields, such as if both consumer_projecd_id and api_key present,
  // one has to set to operation.producer_project_id and the other has to
  // set to label.
  // FillCheckRequest function should copy the strings pointed by info.
  // These buffers may be freed after the FillCheckRequest call.
  utils::Status FillCheckRequest(
      const CheckRequestInfo& info,
      ::google::api::servicecontrol::v1::CheckRequest* request);

  utils::Status FillAllocateQuotaRequest(
      const QuotaRequestInfo& info,
      ::google::api::servicecontrol::v1::AllocateQuotaRequest* request);

  // Fills the CheckRequest protobuf from info.
  // FillReportRequest function should copy the strings pointed by info.
  // These buffers may be freed after the FillReportRequest call.
  utils::Status FillReportRequest(
      const ReportRequestInfo& info,
      ::google::api::servicecontrol::v1::ReportRequest* request);

  // Converts the response status information in the CheckResponse protocol
  // buffer into utils::Status and returns and returns 'check_response_info'
  // subtracted from this CheckResponse.
  // project_id is used when generating error message for project_id related
  // failures.
  static utils::Status ConvertCheckResponse(
      const ::google::api::servicecontrol::v1::CheckResponse& response,
      const std::string& service_name, CheckResponseInfo* check_response_info);

  static utils::Status ConvertAllocateQuotaResponse(
      const ::google::api::servicecontrol::v1::AllocateQuotaResponse& response,
      const std::string& service_name);

  static bool IsMetricSupported(const ::google::api::MetricDescriptor& metric);
  static bool IsLabelSupported(const ::google::api::LabelDescriptor& label);
  const std::string& service_name() const { return service_name_; }
  const std::string& service_config_id() const { return service_config_id_; }

 private:
  const std::vector<std::string> logs_;
  const std::vector<const struct SupportedMetric*> metrics_;
  const std::vector<const struct SupportedLabel*> labels_;
  const std::string service_name_;
  const std::string service_config_id_;
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_PROTO_H_
