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
#include "envoy/server/transport_socket_config.h"

namespace Envoy {
namespace Server {
namespace Configuration {

// ALTS config registry
class AltsTransportSocketConfigFactory
    : public virtual TransportSocketConfigFactory {
 public:
  ProtobufTypes::MessagePtr createEmptyConfigProto() override;
  std::string name() const override;
};

class UpstreamAltsTransportSocketConfigFactory
    : public AltsTransportSocketConfigFactory,
      public UpstreamTransportSocketConfigFactory {
 public:
  Network::TransportSocketFactoryPtr createTransportSocketFactory(
      const Protobuf::Message &, TransportSocketFactoryContext &) override;
};

class DownstreamAltsTransportSocketConfigFactory
    : public AltsTransportSocketConfigFactory,
      public DownstreamTransportSocketConfigFactory {
 public:
  Network::TransportSocketFactoryPtr createTransportSocketFactory(
      const Protobuf::Message &, TransportSocketFactoryContext &,
      const std::vector<std::string> &) override;
};
}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
