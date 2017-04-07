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
#ifndef API_MANAGER_SERVICE_CONTROL_INTERFACE_H_
#define API_MANAGER_SERVICE_CONTROL_INTERFACE_H_

#include "contrib/endpoints/include/api_manager/service_control.h"
#include "contrib/endpoints/include/api_manager/utils/status.h"
#include "contrib/endpoints/src/api_manager/cloud_trace/cloud_trace.h"
#include "contrib/endpoints/src/api_manager/service_control/info.h"

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

  // on_done() function will be called once it is completed.
  // utils::Status in the on_done callback:
  // If status.code is more than 100, it is the HTTP response status
  // from the service control server.
  // If status code is less than 20, within the ranges defined by
  // google/protobuf/stubs/status.h, is from parsing error response
  // body.
  virtual void Quota(const QuotaRequestInfo& info,
                     cloud_trace::CloudTraceSpan* parent_span,
                     std::function<void(utils::Status)> on_done) = 0;

  // Get statistics of ServiceControl library.
  virtual utils::Status GetStatistics(Statistics* stat) const = 0;
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_INTERFACE_H_
