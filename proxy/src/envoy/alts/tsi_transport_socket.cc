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
#include "src/envoy/alts/tsi_transport_socket.h"

#include "common/common/assert.h"
#include "common/common/enum_to_int.h"

namespace Envoy {
namespace Security {

TsiSocket::TsiSocket(HandshakerFactory handshaker_factory,
                     HandshakeValidator handshake_validator)
    : handshaker_factory_(handshaker_factory),
      handshake_validator_(handshake_validator),
      raw_buffer_callbacks_(*this) {
  raw_buffer_socket_.setTransportSocketCallbacks(raw_buffer_callbacks_);
}

TsiSocket::~TsiSocket() { ASSERT(!handshaker_); }

void TsiSocket::setTransportSocketCallbacks(
    Envoy::Network::TransportSocketCallbacks &callbacks) {
  callbacks_ = &callbacks;

  handshaker_ = handshaker_factory_(callbacks.connection().dispatcher());
  handshaker_->setHandshakerCallbacks(*this);
}

std::string TsiSocket::protocol() const { return ""; }

Network::PostIoAction TsiSocket::doHandshake() {
  ASSERT(!handshake_complete_);
  ENVOY_CONN_LOG(debug, "TSI: doHandshake", callbacks_->connection());

  if (!handshaker_next_calling_) {
    doHandshakeNext();
  }
  return Network::PostIoAction::KeepOpen;
}

void TsiSocket::doHandshakeNext() {
  ENVOY_CONN_LOG(debug, "TSI: doHandshake next: received: {}",
                 callbacks_->connection(), raw_read_buffer_.length());
  handshaker_next_calling_ = true;
  Buffer::OwnedImpl handshaker_buffer;
  handshaker_buffer.move(raw_read_buffer_);
  handshaker_->next(handshaker_buffer);
}

Network::PostIoAction TsiSocket::doHandshakeNextDone(
    NextResultPtr &&next_result) {
  ASSERT(next_result);

  ENVOY_CONN_LOG(debug, "TSI: doHandshake next done: status: {} to_send: {}",
                 callbacks_->connection(), next_result->status_,
                 next_result->to_send_->length());

  tsi_result status = next_result->status_;
  tsi_handshaker_result *handshaker_result = next_result->result_.get();

  if (status != TSI_INCOMPLETE_DATA && status != TSI_OK) {
    ENVOY_CONN_LOG(debug, "TSI: Handshake failed: status: {}",
                   callbacks_->connection(), status);
    return Network::PostIoAction::Close;
  }

  if (next_result->to_send_->length() > 0) {
    raw_write_buffer_.move(*next_result->to_send_);
  }

  if (status == TSI_OK && handshaker_result != nullptr) {
    tsi_peer peer;
    tsi_handshaker_result_extract_peer(handshaker_result, &peer);
    ENVOY_CONN_LOG(debug, "TSI: Handshake successful: peer properties: {}",
                   callbacks_->connection(), peer.property_count);
    for (size_t i = 0; i < peer.property_count; ++i) {
      ENVOY_CONN_LOG(debug, "  {}: {}", callbacks_->connection(),
                     peer.properties[i].name,
                     std::string(peer.properties[i].value.data,
                                 peer.properties[i].value.length));
    }
    if (handshake_validator_) {
      std::string err;
      bool peer_validated = handshake_validator_(peer, err);
      if (peer_validated) {
        ENVOY_CONN_LOG(info, "TSI: Handshake validation succeeded.",
                       callbacks_->connection());
      } else {
        ENVOY_CONN_LOG(warn, "TSI: Handshake validation failed: {}",
                       callbacks_->connection(), err);
        tsi_peer_destruct(&peer);
        return Network::PostIoAction::Close;
      }
    } else {
      ENVOY_CONN_LOG(info, "TSI: Handshake validation skipped.",
                     callbacks_->connection());
    }
    tsi_peer_destruct(&peer);

    const unsigned char *unused_bytes;
    size_t unused_byte_size;

    status = tsi_handshaker_result_get_unused_bytes(
        handshaker_result, &unused_bytes, &unused_byte_size);
    ASSERT(status == TSI_OK);
    if (unused_byte_size > 0) {
      raw_read_buffer_.add(unused_bytes, unused_byte_size);
    }
    ENVOY_CONN_LOG(debug, "TSI: Handshake successful: unused_bytes: {}",
                   callbacks_->connection(), unused_byte_size);

    tsi_frame_protector *frame_protector;
    status = tsi_handshaker_result_create_frame_protector(
        handshaker_result, NULL, &frame_protector);
    ASSERT(status == TSI_OK);
    frame_protector_ = std::make_unique<TsiFrameProtector>(frame_protector);

    handshake_complete_ = true;
    callbacks_->raiseEvent(Network::ConnectionEvent::Connected);
  }

  if (raw_read_buffer_.length() > 0) {
    callbacks_->setReadBufferReady();
  }
  return Network::PostIoAction::KeepOpen;
}

Network::IoResult TsiSocket::doRead(Buffer::Instance &buffer) {
  Network::IoResult result = raw_buffer_socket_.doRead(raw_read_buffer_);
  ENVOY_CONN_LOG(debug, "TSI: raw read result action {} bytes {} end_stream {}",
                 callbacks_->connection(), enumToInt(result.action_),
                 result.bytes_processed_, result.end_stream_read_);
  if (result.action_ == Network::PostIoAction::Close &&
      result.bytes_processed_ == 0) {
    return result;
  }

  if (!handshake_complete_) {
    Network::PostIoAction action = doHandshake();
    if (action == Network::PostIoAction::Close || !handshake_complete_) {
      return {action, 0, false};
    }
  }

  if (handshake_complete_) {
    ASSERT(frame_protector_);

    uint64_t read_size = raw_read_buffer_.length();
    ENVOY_CONN_LOG(debug, "TSI: unprotecting buffer size: {}",
                   callbacks_->connection(), raw_read_buffer_.length());
    tsi_result status = frame_protector_->unprotect(raw_read_buffer_, buffer);
    ENVOY_CONN_LOG(debug, "TSI: unprotected buffer left: {} result: {}",
                   callbacks_->connection(), raw_read_buffer_.length(),
                   tsi_result_to_string(status));
    result.bytes_processed_ = read_size - raw_read_buffer_.length();
  }

  ENVOY_CONN_LOG(debug, "TSI: do read result action {} bytes {} end_stream {}",
                 callbacks_->connection(), enumToInt(result.action_),
                 result.bytes_processed_, result.end_stream_read_);
  return result;
}

Network::IoResult TsiSocket::doWrite(Buffer::Instance &buffer,
                                     bool end_stream) {
  if (!handshake_complete_) {
    Network::PostIoAction action = doHandshake();
    if (action == Network::PostIoAction::Close) {
      return {action, 0, false};
    }
  }

  if (handshake_complete_) {
    ASSERT(frame_protector_);
    ENVOY_CONN_LOG(debug, "TSI: protecting buffer size: {}",
                   callbacks_->connection(), buffer.length());
    tsi_result status = frame_protector_->protect(buffer, raw_write_buffer_);
    ENVOY_CONN_LOG(debug, "TSI: protected buffer left: {} result: {}",
                   callbacks_->connection(), buffer.length(),
                   tsi_result_to_string(status));
  }

  ENVOY_CONN_LOG(debug, "TSI: raw_write length {} end_stream {}",
                 callbacks_->connection(), raw_write_buffer_.length(),
                 end_stream);
  return raw_buffer_socket_.doWrite(raw_write_buffer_,
                                    end_stream && (buffer.length() == 0));
}

void TsiSocket::closeSocket(Network::ConnectionEvent) {
  handshaker_.release()->deferredDelete();
}

void TsiSocket::onConnected() { ASSERT(!handshake_complete_); }

void TsiSocket::onNextDone(NextResultPtr &&result) {
  handshaker_next_calling_ = false;

  Network::PostIoAction action = doHandshakeNextDone(std::move(result));
  if (action == Network::PostIoAction::Close) {
    callbacks_->connection().close(Network::ConnectionCloseType::NoFlush);
  }
}

TsiSocketFactory::TsiSocketFactory(HandshakerFactory handshaker_factory,
                                   HandshakeValidator handshake_validator)
    : handshaker_factory_(std::move(handshaker_factory)),
      handshake_validator_(std::move(handshake_validator)) {}

bool TsiSocketFactory::implementsSecureTransport() const { return true; }

Network::TransportSocketPtr TsiSocketFactory::createTransportSocket() const {
  return std::make_unique<TsiSocket>(handshaker_factory_, handshake_validator_);
}
}  // namespace Security
}  // namespace Envoy
