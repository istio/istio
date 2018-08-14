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

#include "proxy/src/envoy/alts/tsi_frame_protector.h"

#include "common/common/assert.h"

namespace Envoy {
namespace Security {

TsiFrameProtector::TsiFrameProtector(tsi_frame_protector *frame_protector)
    : frame_protector_(frame_protector) {}

tsi_result TsiFrameProtector::protect(Buffer::Instance &input,
                                      Buffer::Instance &output) {
  ASSERT(frame_protector_);

  // TODO(lizan): tune size later
  unsigned char protected_buffer[4096];
  size_t protected_buffer_size = sizeof(protected_buffer);
  while (input.length() > 0) {
    auto *message_bytes =
        reinterpret_cast<unsigned char *>(input.linearize(input.length()));
    size_t protected_buffer_size_to_send = protected_buffer_size;
    size_t processed_message_size = input.length();
    tsi_result result = tsi_frame_protector_protect(
        frame_protector_.get(), message_bytes, &processed_message_size,
        protected_buffer, &protected_buffer_size_to_send);
    if (result != TSI_OK) {
      ASSERT(result != TSI_INVALID_ARGUMENT && result != TSI_UNIMPLEMENTED);
      return result;
    }
    output.add(protected_buffer, protected_buffer_size_to_send);
    input.drain(processed_message_size);
  }

  ASSERT(input.length() == 0);
  size_t still_pending_size;
  do {
    size_t protected_buffer_size_to_send = protected_buffer_size;
    tsi_result result = tsi_frame_protector_protect_flush(
        frame_protector_.get(), protected_buffer,
        &protected_buffer_size_to_send, &still_pending_size);
    if (result != TSI_OK) {
      ASSERT(result != TSI_INVALID_ARGUMENT && result != TSI_UNIMPLEMENTED);
      return result;
    }
    output.add(protected_buffer, protected_buffer_size_to_send);
  } while (still_pending_size > 0);

  return TSI_OK;
}

tsi_result TsiFrameProtector::unprotect(Buffer::Instance &input,
                                        Buffer::Instance &output) {
  ASSERT(frame_protector_);

  // TODO(lizan): Tune the buffer size.
  unsigned char unprotected_buffer[4096];
  size_t unprotected_buffer_size = sizeof(unprotected_buffer);

  while (input.length() > 0) {
    auto *message_bytes =
        reinterpret_cast<unsigned char *>(input.linearize(input.length()));
    size_t unprotected_buffer_size_to_send = unprotected_buffer_size;
    size_t processed_message_size = input.length();
    tsi_result result = tsi_frame_protector_unprotect(
        frame_protector_.get(), message_bytes, &processed_message_size,
        unprotected_buffer, &unprotected_buffer_size_to_send);
    if (result != TSI_OK) {
      ASSERT(result != TSI_INVALID_ARGUMENT && result != TSI_UNIMPLEMENTED);
      return result;
    }
    output.add(unprotected_buffer, unprotected_buffer_size_to_send);
    input.drain(processed_message_size);
  }

  return TSI_OK;
}

}  // namespace Security
}  // namespace Envoy
