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

#include "protobuf.h"

using namespace std::chrono;

using ::google::protobuf::Duration;
using ::google::protobuf::Timestamp;

using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {
namespace {

// The error string prefix for Invalid dictionary.
const char* kInvalidDictionaryErrorPrefix =
    "Request could not be processed due to invalid";

}  // namespace

// Convert timestamp from time_point to Timestamp
Timestamp CreateTimestamp(system_clock::time_point tp) {
  Timestamp time_stamp;
  long long nanos = duration_cast<nanoseconds>(tp.time_since_epoch()).count();

  time_stamp.set_seconds(nanos / 1000000000);
  time_stamp.set_nanos(nanos % 1000000000);
  return time_stamp;
}

Duration CreateDuration(nanoseconds value) {
  Duration duration;
  duration.set_seconds(value.count() / 1000000000);
  duration.set_nanos(value.count() % 1000000000);
  return duration;
}

milliseconds ToMilliseonds(const Duration& duration) {
  return duration_cast<milliseconds>(seconds(duration.seconds())) +
         duration_cast<milliseconds>(nanoseconds(duration.nanos()));
}

bool InvalidDictionaryStatus(const Status& status) {
  return status.error_code() == Code::INVALID_ARGUMENT &&
         status.error_message().starts_with(kInvalidDictionaryErrorPrefix);
}

}  // namespace mixer_client
}  // namespace istio
