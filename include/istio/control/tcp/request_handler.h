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

#ifndef ISTIO_CONTROL_TCP_REQUEST_HANDLER_H
#define ISTIO_CONTROL_TCP_REQUEST_HANDLER_H

#include "include/istio/control/tcp/check_data.h"
#include "include/istio/control/tcp/report_data.h"
#include "include/istio/mixerclient/client.h"

namespace istio {
namespace control {
namespace tcp {

// The interface to handle a TCP request.
class RequestHandler {
 public:
  virtual ~RequestHandler() {}

  // Perform a Check call. It will:
  // * extract downstream tcp connection attributes
  // * check config, make a Check call if necessary.
  virtual ::istio::mixerclient::CancelFunc Check(
      CheckData* check_data, ::istio::mixerclient::DoneFunc on_done) = 0;

  // Make report call.
  // This can be called multiple times for long connection.
  // TODO(JimmyCYJ): Let TCP filter use
  // void Report(ReportData* report_data, bool is_final_report), and deprecate
  // this method.
  virtual void Report(ReportData* report_data) = 0;

  // Make report call.
  // If is_final_report is true, report all attributes. Otherwise, report delta
  // attributes.
  virtual void Report(ReportData* report_data, bool is_final_report) = 0;
};

}  // namespace tcp
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_TCP_REQUEST_HANDLER_H
