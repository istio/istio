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
#ifndef API_MANAGER_SERVICE_CONTROL_INTERFACE_H_
#define API_MANAGER_SERVICE_CONTROL_INTERFACE_H_

#include "include/api_manager/service_control.h"
#include "include/api_manager/utils/status.h"
#include "src/api_manager/cloud_trace/cloud_trace.h"
#include "src/api_manager/service_control/info.h"

namespace google {
namespace api_manager {
namespace service_control {

// An interface to support calling Service Control services.
class Interface {
 public:
  virtual ~Interface() {}

  // Init() should be called before following calls
  //   Report
  //   Check
  //   GetStatistics
  // But Init() should be called after key functions of ApiManagerEnvInterface
  // are ready, such as Log, and StartPeriodicTimer.
  // Specifically for Nginx, the object can be created at master process
  // at config or postconfig. But Init() should be called at init_process
  // of each worker.
  virtual utils::Status Init() = 0;

  // Flushs out all items in the cache and close the object.
  // After Close() is called, following methods should not be used.
  //   Report
  //   Check
  //   GetStatistics
  // This method can be called repeatedly.
  virtual utils::Status Close() = 0;

  // Sends a ServiceControl Report.
  // Report calls are always aggregated and cached.
  // Return utils::Status usually is OK unless some caching error.
  // If status code is less than 20, as defined by
  // google/protobuf/stubs/status.h,
  // then the error is from processing response fields (e.g. INVALID_ARGUMENT).
  // Reports may be sent to the service control server asynchronously if caching
  // is enabled. HTTP request errors carry Nginx error code.
  virtual utils::Status Report(const ReportRequestInfo& info) = 0;

  // Sends ServiceControl Check asynchronously.
  // on_done() function will be called once it is completed.
  // utils::Status in the on_done callback:
  // If status.code is more than 100, it is the HTTP response status
  // from the service control server.
  // If status code is less than 20, within the ranges defined by
  // google/protobuf/stubs/status.h, is from parsing error response
  // body.
  virtual void Check(
      const CheckRequestInfo& info, cloud_trace::CloudTraceSpan* parent_span,
      std::function<void(utils::Status, const CheckResponseInfo&)> on_done) = 0;

  // Get statistics of ServiceControl library.
  virtual utils::Status GetStatistics(Statistics* stat) const = 0;
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_INTERFACE_H_
