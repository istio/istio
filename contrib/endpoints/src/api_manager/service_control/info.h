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
#ifndef API_MANAGER_SERVICE_CONTROL_INFO_H_
#define API_MANAGER_SERVICE_CONTROL_INFO_H_

#include "google/protobuf/stubs/stringpiece.h"

#include "google/api/quota.pb.h"

#include <time.h>
#include <chrono>
#include <memory>
#include <string>

#include "contrib/endpoints/include/api_manager/compute_platform.h"
#include "contrib/endpoints/include/api_manager/protocol.h"
#include "contrib/endpoints/include/api_manager/service_control.h"
#include "contrib/endpoints/include/api_manager/utils/status.h"

namespace google {
namespace api_manager {
namespace service_control {

// Use the CheckRequestInfo and ReportRequestInfo to fill Service Control
// request protocol buffers. Use following two structures to pass
// in minimum info and call Fill functions to fill the protobuf.

// Basic information about the API call (operation).
struct OperationInfo {
  // Identity of the operation. It must be unique within the scope of the
  // service. If the service calls Check() and Report() on the same operation,
  // the two calls should carry the same operation id.
  ::google::protobuf::StringPiece operation_id;

  // Fully qualified name of the operation.
  ::google::protobuf::StringPiece operation_name;

  // The producer project id.
  ::google::protobuf::StringPiece producer_project_id;

  // The API key.
  ::google::protobuf::StringPiece api_key;

  // Uses Referer header, if the Referer header doesn't present, use the
  // Origin header. If both of them not present, it's empty.
  ::google::protobuf::StringPiece referer;

  // The start time of the call. Used to set operation.start_time for both Check
  // and Report.
  std::chrono::system_clock::time_point request_start_time;

  // The client IP address.
  std::string client_ip;

  // The client host name.
  std::string client_host;

  OperationInfo() {}
};

// Information to fill Check request protobuf.
struct CheckRequestInfo : public OperationInfo {
  // Whether the method allow unregistered calls.
  bool allow_unregistered_calls;

  // used for api key restriction check
  std::string android_package_name;
  std::string android_cert_fingerprint;
  std::string ios_bundle_id;

  CheckRequestInfo() : allow_unregistered_calls(false) {}
};

// Stores the information substracted from the check response.
struct CheckResponseInfo {
  // If the request have a valid api key.
  bool is_api_key_valid;
  // If service is activated.
  bool service_is_activated;

  // By default api_key is valid and service is activated.
  // They only set to false by the check response from server.
  CheckResponseInfo() : is_api_key_valid(true), service_is_activated(true) {}
};

struct QuotaRequestInfo : public OperationInfo {
  std::string method_name;

  const std::vector<std::pair<std::string, int>>* metric_cost_vector;
};

// Information to fill Report request protobuf.
struct ReportRequestInfo : public OperationInfo {
  // The HTTP response code.
  int response_code;

  // The response status.
  utils::Status status;

  // Original request URL.
  std::string url;

  // location of the service, such as us-central.
  ::google::protobuf::StringPiece location;
  // API name and version.
  ::google::protobuf::StringPiece api_name;
  ::google::protobuf::StringPiece api_version;
  ::google::protobuf::StringPiece api_method;

  // The request size in bytes. -1 if not available.
  int64_t request_size;

  // The response size in bytes. -1 if not available.
  int64_t response_size;

  // per request latency.
  LatencyInfo latency;

  // The message to log as INFO log.
  std::string log_message;

  // Auth info: issuer and audience.
  std::string auth_issuer;
  std::string auth_audience;

  // Protocol used to issue the request.
  protocol::Protocol frontend_protocol;
  protocol::Protocol backend_protocol;

  // HTTP method. all-caps string such as "GET", "POST" etc.
  std::string method;

  // A recognized compute platform (GAE, GCE, GKE).
  compute_platform::ComputePlatform compute_platform;

  // If consumer data should be sent.
  CheckResponseInfo check_response_info;

  // request message size till the current time point.
  int64_t request_bytes;

  // response message size till the current time point.
  int64_t response_bytes;

  // number of messages for a stream.
  int64_t streaming_request_message_counts;

  // number of messages for a stream.
  int64_t streaming_response_message_counts;

  // streaming duration (us) between first message and last message.
  int64_t streaming_durations;

  // Flag to indicate the first report
  bool is_first_report;

  // Flag to indicate the final report
  bool is_final_report;

  ReportRequestInfo()
      : response_code(200),
        status(utils::Status::OK),
        request_size(-1),
        response_size(-1),
        frontend_protocol(protocol::UNKNOWN),
        backend_protocol(protocol::UNKNOWN),
        compute_platform(compute_platform::UNKNOWN),
        request_bytes(0),
        response_bytes(0),
        streaming_request_message_counts(0),
        streaming_response_message_counts(0),
        streaming_durations(0),
        is_first_report(true),
        is_final_report(true) {}
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_INFO_H_
