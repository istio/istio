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

#include "include/istio/utils/attribute_names.h"
#include "include/istio/utils/attributes_builder.h"

namespace istio {
namespace control {
namespace tcp {
namespace {
// Connection events for TCP connection.
const std::string kConnectionOpen("open");
const std::string kConnectionContinue("continue");
const std::string kConnectionClose("close");
}  // namespace

void AttributesBuilder::ExtractCheckAttributes(CheckData* check_data) {
  utils::AttributesBuilder builder(&request_->attributes);

  std::string source_ip;
  int source_port;
  // TODO(kuat): there is no way to propagate source IP in TCP, so we auto-set
  // it
  if (check_data->GetSourceIpPort(&source_ip, &source_port)) {
    builder.AddBytes(utils::AttributeName::kSourceIp, source_ip);
    // connection remote IP is always reported as origin IP
    builder.AddBytes(utils::AttributeName::kOriginIp, source_ip);
  }

  // TODO(diemtvu): add TCP authn filter similar to http case, and use authn
  // result output here instead.
  std::string source_user;
  if (check_data->GetPrincipal(true, &source_user)) {
    // TODO(diemtvu): remove kSourceUser once migration to source.principal is
    // over. https://github.com/istio/istio/issues/4689
    builder.AddString(utils::AttributeName::kSourceUser, source_user);
    builder.AddString(utils::AttributeName::kSourcePrincipal, source_user);
  }

  std::string destination_principal;
  if (check_data->GetPrincipal(false, &destination_principal)) {
    builder.AddString(utils::AttributeName::kDestinationPrincipal,
                      destination_principal);
  }

  builder.AddBool(utils::AttributeName::kConnectionMtls,
                  check_data->IsMutualTLS());

  std::string requested_server_name;
  if (check_data->GetRequestedServerName(&requested_server_name)) {
    builder.AddString(utils::AttributeName::kConnectionRequestedServerName,
                      requested_server_name);
  }

  builder.AddTimestamp(utils::AttributeName::kContextTime,
                       std::chrono::system_clock::now());
  builder.AddString(utils::AttributeName::kContextProtocol, "tcp");

  // Get unique downstream connection ID, which is <uuid>-<connection id>.
  std::string connection_id = check_data->GetConnectionId();
  builder.AddString(utils::AttributeName::kConnectionId, connection_id);
}

void AttributesBuilder::ExtractReportAttributes(
    ReportData* report_data, ReportData::ConnectionEvent event,
    ReportData::ReportInfo* last_report_info) {
  utils::AttributesBuilder builder(&request_->attributes);

  ReportData::ReportInfo info;
  report_data->GetReportInfo(&info);
  builder.AddInt64(utils::AttributeName::kConnectionReceviedBytes,
                   info.received_bytes - last_report_info->received_bytes);
  builder.AddInt64(utils::AttributeName::kConnectionReceviedTotalBytes,
                   info.received_bytes);
  builder.AddInt64(utils::AttributeName::kConnectionSendBytes,
                   info.send_bytes - last_report_info->send_bytes);
  builder.AddInt64(utils::AttributeName::kConnectionSendTotalBytes,
                   info.send_bytes);

  if (event == ReportData::ConnectionEvent::CLOSE) {
    builder.AddDuration(utils::AttributeName::kConnectionDuration,
                        info.duration);
    if (!request_->check_status.ok()) {
      builder.AddInt64(utils::AttributeName::kCheckErrorCode,
                       request_->check_status.error_code());
      builder.AddString(utils::AttributeName::kCheckErrorMessage,
                        request_->check_status.ToString());
    }
    builder.AddString(utils::AttributeName::kConnectionEvent, kConnectionClose);
  } else if (event == ReportData::ConnectionEvent::OPEN) {
    builder.AddString(utils::AttributeName::kConnectionEvent, kConnectionOpen);
  } else {
    last_report_info->received_bytes = info.received_bytes;
    last_report_info->send_bytes = info.send_bytes;
    builder.AddString(utils::AttributeName::kConnectionEvent,
                      kConnectionContinue);
  }

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

  builder.AddTimestamp(utils::AttributeName::kContextTime,
                       std::chrono::system_clock::now());
}

}  // namespace tcp
}  // namespace control
}  // namespace istio
