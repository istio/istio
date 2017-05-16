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

#include "src/envoy/transcoding/envoy_input_stream.h"

namespace Envoy {
namespace Grpc {

void EnvoyInputStream::Move(Buffer::Instance &instance) {
  if (!finished_) {
    buffer_.move(instance);
  }
}

bool EnvoyInputStream::Next(const void **data, int *size) {
  if (position_ != 0) {
    buffer_.drain(position_);
    position_ = 0;
  }

  Buffer::RawSlice slice;
  uint64_t num_slices = buffer_.getRawSlices(&slice, 1);

  if (num_slices) {
    *data = slice.mem_;
    *size = slice.len_;
    position_ = slice.len_;
    byte_count_ += slice.len_;
    return true;
  }

  if (!finished_) {
    *data = nullptr;
    *size = 0;
    return true;
  }
  return false;
}

void EnvoyInputStream::BackUp(int count) {
  GOOGLE_CHECK_GE(count, 0);
  GOOGLE_CHECK_LE(count, position_);

  position_ -= count;
  byte_count_ -= count;
}

int64_t EnvoyInputStream::BytesAvailable() const {
  return buffer_.length() - position_;
}

}  // namespace Grpc
}  // namespace Envoy
