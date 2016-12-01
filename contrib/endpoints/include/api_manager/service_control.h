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
