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

#include "attributes_builder.h"

#include "control/src/attribute_names.h"
#include "include/attributes_builder.h"

namespace istio {
namespace mixer_control {
namespace tcp {

void AttributesBuilder::ExtractCheckAttributes(CheckData* check_data) {
  ::istio::mixer_client::AttributesBuilder builder(&request_->attributes);

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
  builder.AddTimestamp(AttributeName::kContextTime,
                       std::chrono::system_clock::now());
  builder.AddString(AttributeName::kContextProtocol, "tcp");
}

void AttributesBuilder::ExtractReportAttributes(ReportData* report_data) {
  ::istio::mixer_client::AttributesBuilder builder(&request_->attributes);

  ReportData::ReportInfo info;
  report_data->GetReportInfo(&info);
  builder.AddInt64(AttributeName::kConnectionReceviedBytes,
                   info.received_bytes);
  builder.AddInt64(AttributeName::kConnectionReceviedTotalBytes,
                   info.received_bytes);
  builder.AddInt64(AttributeName::kConnectionSendBytes, info.send_bytes);
  builder.AddInt64(AttributeName::kConnectionSendTotalBytes, info.send_bytes);
  builder.AddDuration(AttributeName::kConnectionDuration, info.duration);
  builder.AddInt64(AttributeName::kCheckStatusCode,
                   request_->check_status.error_code());

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
}  // namespace mixer_control
}  // namespace istio
