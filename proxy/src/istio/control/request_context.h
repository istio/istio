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

#ifndef ISTIO_CONTROL_REQUEST_CONTEXT_H
#define ISTIO_CONTROL_REQUEST_CONTEXT_H

#include "google/protobuf/stubs/status.h"
#include "mixer/v1/attributes.pb.h"
#include "proxy/include/istio/quota_config/requirement.h"

#include <vector>

namespace istio {
namespace control {

// The context to hold request data for both HTTP and TCP.
struct RequestContext {
  // The attributes for both Check and Report.
  ::istio::mixer::v1::Attributes attributes;
  // The quota requirements
  std::vector<::istio::quota_config::Requirement> quotas;
  // The check status.
  ::google::protobuf::util::Status check_status;
};

}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_REQUEST_CONTEXT_H
