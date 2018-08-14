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
#pragma once

#include "common/buffer/buffer_impl.h"
#include "common/network/raw_buffer_socket.h"
#include "envoy/network/transport_socket.h"
#include "proxy/src/envoy/alts/tsi_frame_protector.h"
#include "proxy/src/envoy/alts/tsi_handshaker.h"

namespace Envoy {
namespace Security {

typedef std::function<TsiHandshakerPtr(Event::Dispatcher&)> HandshakerFactory;

/**
 * A function to validate the peer of the connection.
 * @param peer the detail peer information of the connection.
 * @param err an error message to indicate why the peer is invalid. This is an
 * output param that should be populated by the function implementation.
 * @return true if the peer is valid or false if the peer is invalid.
 */
typedef std::function<bool(const tsi_peer& peer, std::string& err)>
    HandshakeValidator;

/**
 * A implementation of Network::TransportSocket based on gRPC TSI
 */
class TsiSocket : public Network::TransportSocket,
                  public TsiHandshakerCallbacks,
                  public Logger::Loggable<Logger::Id::connection> {
 public:
  /**
   * @param handshaker_factory a function to initiate a TsiHandshaker
   * @param handshake_validator a function to validate the peer. Called right
   * after the handshake completed with peer data to do the peer validation.
   * The connection will be closed immediately if it returns false.
   */
  TsiSocket(HandshakerFactory handshaker_factory,
            HandshakeValidator handshake_validator);
  virtual ~TsiSocket();

  // Network::TransportSocket
  void setTransportSocketCallbacks(
      Envoy::Network::TransportSocketCallbacks& callbacks) override;
  std::string protocol() const override;
  bool canFlushClose() override { return handshake_complete_; }
  Envoy::Ssl::Connection* ssl() override { return nullptr; }
  const Envoy::Ssl::Connection* ssl() const override { return nullptr; }
  Network::IoResult doWrite(Buffer::Instance& buffer, bool end_stream) override;
  void closeSocket(Network::ConnectionEvent event) override;
  Network::IoResult doRead(Buffer::Instance& buffer) override;
  void onConnected() override;

  // TsiHandshakerCallbacks
  void onNextDone(NextResultPtr&& result) override;

 private:
  /**
   * Callbacks for underlying RawBufferSocket, it proxies fd() and connection()
   * but not raising event or flow control since they have to be handled in
   * TsiSocket.
   */
  class RawBufferCallbacks : public Network::TransportSocketCallbacks {
   public:
    explicit RawBufferCallbacks(TsiSocket& parent) : parent_(parent) {}

    int fd() const override { return parent_.callbacks_->fd(); }
    Network::Connection& connection() override {
      return parent_.callbacks_->connection();
    }
    bool shouldDrainReadBuffer() override { return false; }
    void setReadBufferReady() override {}
    void raiseEvent(Network::ConnectionEvent) override {}

   private:
    TsiSocket& parent_;
  };

  Network::PostIoAction doHandshake();
  void doHandshakeNext();
  Network::PostIoAction doHandshakeNextDone(NextResultPtr&& next_result);

  HandshakerFactory handshaker_factory_;
  HandshakeValidator handshake_validator_;
  TsiHandshakerPtr handshaker_{};
  bool handshaker_next_calling_{};
  // TODO(lizan): wrap frame protector in a C++ class
  TsiFrameProtectorPtr frame_protector_;

  Envoy::Network::TransportSocketCallbacks* callbacks_{};
  RawBufferCallbacks raw_buffer_callbacks_;
  Network::RawBufferSocket raw_buffer_socket_;

  Envoy::Buffer::OwnedImpl raw_read_buffer_;
  Envoy::Buffer::OwnedImpl raw_write_buffer_;
  bool handshake_complete_{};
};

/**
 * An implementation of Network::TransportSocketFactory for TsiSocket
 */
class TsiSocketFactory : public Network::TransportSocketFactory {
 public:
  TsiSocketFactory(HandshakerFactory handshaker_factory,
                   HandshakeValidator handshake_validator);

  bool implementsSecureTransport() const override;
  Network::TransportSocketPtr createTransportSocket() const override;

 private:
  HandshakerFactory handshaker_factory_;
  HandshakeValidator handshake_validator_;
};
}  // namespace Security
}  // namespace Envoy
