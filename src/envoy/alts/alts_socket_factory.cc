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

#include "src/envoy/alts/alts_socket_factory.h"
#include "common/common/assert.h"
#include "common/protobuf/protobuf.h"
#include "common/protobuf/utility.h"
#include "envoy/registry/registry.h"
#include "envoy/server/transport_socket_config.h"
#include "grpc/grpc_security.h"
#include "src/core/tsi/alts/handshaker/alts_tsi_handshaker.h"
#include "src/envoy/alts/alts_socket.pb.h"
#include "src/envoy/alts/alts_socket.pb.validate.h"
#include "src/envoy/alts/tsi_handshaker.h"
#include "src/envoy/alts/tsi_transport_socket.h"

namespace Envoy {
namespace Server {
namespace Configuration {

ProtobufTypes::MessagePtr
AltsTransportSocketConfigFactory::createEmptyConfigProto() {
  return std::make_unique<envoy::security::v2::AltsSocket>();
}

std::string
Envoy::Server::Configuration::AltsTransportSocketConfigFactory::name() const {
  return "alts";
}

Network::TransportSocketFactoryPtr
UpstreamAltsTransportSocketConfigFactory::createTransportSocketFactory(
    const Protobuf::Message &message, TransportSocketFactoryContext &) {
  auto config =
      MessageUtil::downcastAndValidate<const envoy::security::v2::AltsSocket &>(
          message);

  std::string handshaker_service = config.handshaker_service();

  return std::make_unique<Security::TsiSocketFactory>(
      [handshaker_service](Event::Dispatcher &dispatcher) {
        grpc_alts_credentials_options *options =
            grpc_alts_credentials_client_options_create();

        tsi_handshaker *handshaker = nullptr;

        // Specifying target name as empty since TSI won't take care of
        // validating peer identity in this use case. The validation will be
        // implemented in TsiSocket later.
        alts_tsi_handshaker_create(options, "", handshaker_service.c_str(),
                                   true /* is_client */, &handshaker);

        ASSERT(handshaker != nullptr);

        grpc_alts_credentials_options_destroy(options);

        return std::make_unique<Security::TsiHandshaker>(handshaker,
                                                         dispatcher);
      });
}

Network::TransportSocketFactoryPtr
DownstreamAltsTransportSocketConfigFactory::createTransportSocketFactory(
    const std::string &, const std::vector<std::string> &, bool,
    const Protobuf::Message &message, TransportSocketFactoryContext &) {
  auto config =
      MessageUtil::downcastAndValidate<const envoy::security::v2::AltsSocket &>(
          message);

  std::string handshaker_service = config.handshaker_service();

  return std::make_unique<Security::TsiSocketFactory>(
      [handshaker_service](Event::Dispatcher &dispatcher) {
        grpc_alts_credentials_options *options =
            grpc_alts_credentials_server_options_create();

        tsi_handshaker *handshaker = nullptr;

        alts_tsi_handshaker_create(options, nullptr, handshaker_service.c_str(),
                                   false /* is_client */, &handshaker);

        ASSERT(handshaker != nullptr);

        grpc_alts_credentials_options_destroy(options);

        return std::make_unique<Security::TsiHandshaker>(handshaker,
                                                         dispatcher);
      });
}

static Registry::RegisterFactory<UpstreamAltsTransportSocketConfigFactory,
                                 UpstreamTransportSocketConfigFactory>
    upstream_registered_;

static Registry::RegisterFactory<DownstreamAltsTransportSocketConfigFactory,
                                 DownstreamTransportSocketConfigFactory>
    downstream_registered_;
}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
