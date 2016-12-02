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
#ifndef API_MANAGER_SERVICE_CONTROL_H_
#define API_MANAGER_SERVICE_CONTROL_H_

namespace google {
namespace api_manager {
namespace service_control {

// The statistics recorded by service control library.
// Important note: please don't use std::string. These fields are directly
// copied into a shared memory.
struct Statistics {
  // Total number of Check() calls received.
  uint64_t total_called_checks;
  // Check sends to server from flushed cache items.
  uint64_t send_checks_by_flush;
  // Check sends to remote sever during Check() calls.
  uint64_t send_checks_in_flight;

  // Total number of Report() calls received.
  uint64_t total_called_reports;
  // Report sends to server from flushed cache items.
  uint64_t send_reports_by_flush;
  // Report sends to remote sever during Report() calls.
  uint64_t send_reports_in_flight;

  // The number of operations send, each input report has only 1 operation, but
  // each report send to server may have multiple operations. The ratio of
  // send_report_operations / total_called_reports  will reflect report
  // aggregation rate.  send_report_operations may not reflect aggregation rate.
  uint64_t send_report_operations;

  // Maximum report request size send to server.
  uint64_t max_report_size;
};

// Per request latency statistics.
struct LatencyInfo {
  // The request time in milliseconds. -1 if not available.
  int64_t request_time_ms;
  // The backend request time in milliseconds. -1 if not available.
  int64_t backend_time_ms;
  // The API Manager overhead time in milliseconds. -1 if not available.
  int64_t overhead_time_ms;

  LatencyInfo()
      : request_time_ms(-1), backend_time_ms(-1), overhead_time_ms(-1) {}
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_H_
