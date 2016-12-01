/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */
#ifndef API_MANAGER_SERVICE_CONTROL_PROTO_H_
#define API_MANAGER_SERVICE_CONTROL_PROTO_H_

#include "google/api/label.pb.h"
#include "google/api/metric.pb.h"
#include "google/api/servicecontrol/v1/service_controller.pb.h"
#include "include/api_manager/utils/status.h"
#include "src/api_manager/service_control/info.h"

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
