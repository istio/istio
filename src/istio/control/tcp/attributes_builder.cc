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

#include "src/istio/control/tcp/attributes_builder.h"

#include "include/istio/utils/attributes_builder.h"
#include "src/istio/control/attribute_names.h"

namespace istio {
namespace control {
namespace tcp {

void AttributesBuilder::ExtractCheckAttributes(CheckData* check_data) {
  utils::AttributesBuilder builder(&request_->attributes);

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
  builder.AddBool(AttributeName::kConnectionMtls, check_data->IsMutualTLS());

  builder.AddTimestamp(AttributeName::kContextTime,
                       std::chrono::system_clock::now());
  builder.AddString(AttributeName::kContextProtocol, "tcp");

  // Get unique downstream connection ID, which is <uuid>-<connection id>.
  std::string connection_id = check_data->GetConnectionId();
  builder.AddString(AttributeName::kConnectionId, connection_id);
}

void AttributesBuilder::ExtractReportAttributes(
    ReportData* report_data, bool is_final_report,
    ReportData::ReportInfo* last_report_info) {
  utils::AttributesBuilder builder(&request_->attributes);

  ReportData::ReportInfo info;
  report_data->GetReportInfo(&info);
  builder.AddInt64(AttributeName::kConnectionReceviedBytes,
                   info.received_bytes - last_report_info->received_bytes);
  builder.AddInt64(AttributeName::kConnectionReceviedTotalBytes,
                   info.received_bytes);
  builder.AddInt64(AttributeName::kConnectionSendBytes,
                   info.send_bytes - last_report_info->send_bytes);
  builder.AddInt64(AttributeName::kConnectionSendTotalBytes, info.send_bytes);

  if (is_final_report) {
    builder.AddDuration(AttributeName::kConnectionDuration, info.duration);
    if (!request_->check_status.ok()) {
      builder.AddInt64(AttributeName::kCheckErrorCode,
                       request_->check_status.error_code());
      builder.AddString(AttributeName::kCheckErrorMessage,
                        request_->check_status.ToString());
    }
  } else {
    last_report_info->received_bytes = info.received_bytes;
    last_report_info->send_bytes = info.send_bytes;
  }

  std::string dest_ip;
  int dest_port;
  if (report_data->GetDestinationIpPort(&dest_ip, &dest_port)) {
    builder.AddBytes(AttributeName::kDestinationIp, dest_ip);
    builder.AddInt64(AttributeName::kDestinationPort, dest_port);
  }

  builder.AddTimestamp(AttributeName::kContextTime,
                       std::chrono::system_clock::now());
}

}  // namespace tcp
}  // namespace control
}  // namespace istio
