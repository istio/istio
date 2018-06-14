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

#pragma once

#include "envoy/event/dispatcher.h"
#include "envoy/runtime/runtime.h"
#include "envoy/upstream/cluster_manager.h"
#include "include/istio/mixerclient/client.h"
#include "src/envoy/utils/config.h"

using ::istio::mixer::v1::Attributes;

namespace Envoy {
namespace Utils {

// Create all environment functions for mixerclient
void CreateEnvironment(Event::Dispatcher &dispatcher,
                       Runtime::RandomGenerator &random,
                       Grpc::AsyncClientFactory &check_client_factory,
                       Grpc::AsyncClientFactory &report_client_factory,
                       const std::string &serialized_forward_attributes,
                       ::istio::mixerclient::Environment *env);

void SerializeForwardedAttributes(
    const ::istio::mixer::v1::config::client::TransportConfig &transport,
    std::string *serialized_forward_attributes);

Grpc::AsyncClientFactoryPtr GrpcClientFactoryForCluster(
    const std::string &cluster_name, Upstream::ClusterManager &cm,
    Stats::Scope &scope);

}  // namespace Utils
}  // namespace Envoy
