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
#include "proxy/src/envoy/alts/tsi_handshaker.h"
#include "common/buffer/buffer_impl.h"
#include "common/common/assert.h"

namespace Envoy {
namespace Security {

void TsiHandshaker::onNextDone(tsi_result status, void *user_data,
                               const unsigned char *bytes_to_send,
                               size_t bytes_to_send_size,
                               tsi_handshaker_result *handshaker_result) {
  TsiHandshaker *handshaker = static_cast<TsiHandshaker *>(user_data);

  Buffer::InstancePtr to_send = std::make_unique<Buffer::OwnedImpl>();
  if (bytes_to_send_size > 0) {
    to_send->add(bytes_to_send, bytes_to_send_size);
  }

  auto next_result = new TsiHandshakerCallbacks::NextResult{
      status, std::move(to_send), {handshaker_result}};

  handshaker->dispatcher_.post([handshaker, next_result]() {
    ASSERT(handshaker->calling_);
    handshaker->calling_ = false;

    TsiHandshakerCallbacks::NextResultPtr next_result_ptr{next_result};

    if (handshaker->delete_on_done_) {
      handshaker->dispatcher_.deferredDelete(
          Event::DeferredDeletablePtr{handshaker});
      return;
    }
    handshaker->callbacks_->onNextDone(std::move(next_result_ptr));
  });
}

TsiHandshaker::TsiHandshaker(tsi_handshaker *handshaker,
                             Event::Dispatcher &dispatcher)
    : handshaker_(handshaker), dispatcher_(dispatcher) {}

TsiHandshaker::~TsiHandshaker() { ASSERT(!calling_); }

tsi_result TsiHandshaker::next(Envoy::Buffer::Instance &received) {
  ASSERT(!calling_);
  calling_ = true;

  uint64_t received_size = received.length();
  const unsigned char *bytes_to_send = nullptr;
  size_t bytes_to_send_size = 0;
  tsi_handshaker_result *result = nullptr;
  tsi_result status =
      tsi_handshaker_next(handshaker_.get(),
                          reinterpret_cast<const unsigned char *>(
                              received.linearize(received_size)),
                          received_size, &bytes_to_send, &bytes_to_send_size,
                          &result, onNextDone, this);

  if (status != TSI_ASYNC) {
    onNextDone(status, this, bytes_to_send, bytes_to_send_size, result);
  }
  return status;
}

void TsiHandshaker::deferredDelete() {
  if (calling_) {
    delete_on_done_ = true;
  } else {
    dispatcher_.deferredDelete(Event::DeferredDeletablePtr{this});
  }
}
}  // namespace Security
}  // namespace Envoy
