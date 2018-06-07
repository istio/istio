/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#ifndef ISTIO_MIXERCLIENT_CHECK_RESPONSE_H
#define ISTIO_MIXERCLIENT_CHECK_RESPONSE_H

#include "google/protobuf/stubs/status.h"
#include "mixer/v1/check.pb.h"

namespace istio {
namespace mixerclient {

// The CheckResponseInfo holds response information in detail.
struct CheckResponseInfo {
  // Whether this check response is from cache.
  bool is_check_cache_hit{false};

  // Whether this quota response is from cache.
  bool is_quota_cache_hit{false};

  // The check and quota response status.
  ::google::protobuf::util::Status response_status{
      ::google::protobuf::util::Status::UNKNOWN};

  // Routing directive (applicable if the status is OK)
  ::istio::mixer::v1::RouteDirective route_directive{
      ::istio::mixer::v1::RouteDirective::default_instance()};
};

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_CHECK_RESPONSE_H
