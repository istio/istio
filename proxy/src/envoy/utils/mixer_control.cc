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

#include "proxy/src/envoy/utils/mixer_control.h"
#include "proxy/src/envoy/utils/grpc_transport.h"

using ::istio::mixerclient::Statistics;

namespace Envoy {
namespace Utils {
namespace {

// A class to wrap envoy timer for mixer client timer.
class EnvoyTimer : public ::istio::mixerclient::Timer {
 public:
  EnvoyTimer(Event::TimerPtr timer) : timer_(std::move(timer)) {}

  void Stop() override { timer_->disableTimer(); }
  void Start(int interval_ms) override {
    timer_->enableTimer(std::chrono::milliseconds(interval_ms));
  }

 private:
  Event::TimerPtr timer_;
};

// Fork of Envoy::Grpc::AsyncClientFactoryImpl, workaround for
// https://github.com/envoyproxy/envoy/issues/2762
class EnvoyGrpcAsyncClientFactory : public Grpc::AsyncClientFactory {
 public:
  EnvoyGrpcAsyncClientFactory(Upstream::ClusterManager &cm,
                              envoy::api::v2::core::GrpcService config)
      : cm_(cm), config_(config) {}

  Grpc::AsyncClientPtr create() override {
    return std::make_unique<Grpc::AsyncClientImpl>(cm_, config_);
  }

 private:
  Upstream::ClusterManager &cm_;
  envoy::api::v2::core::GrpcService config_;
};

}  // namespace

// Create all environment functions for mixerclient
void CreateEnvironment(Event::Dispatcher &dispatcher,
                       Runtime::RandomGenerator &random,
                       Grpc::AsyncClientFactory &check_client_factory,
                       Grpc::AsyncClientFactory &report_client_factory,
                       const std::string &serialized_forward_attributes,
                       ::istio::mixerclient::Environment *env) {
  env->check_transport = CheckTransport::GetFunc(check_client_factory,
                                                 Tracing::NullSpan::instance(),
                                                 serialized_forward_attributes);
  env->report_transport = ReportTransport::GetFunc(
      report_client_factory, Tracing::NullSpan::instance(),
      serialized_forward_attributes);

  env->timer_create_func = [&dispatcher](std::function<void()> timer_cb)
      -> std::unique_ptr<::istio::mixerclient::Timer> {
    return std::unique_ptr<::istio::mixerclient::Timer>(
        new EnvoyTimer(dispatcher.createTimer(timer_cb)));
  };

  env->uuid_generate_func = [&random]() -> std::string {
    return random.uuid();
  };
}

void SerializeForwardedAttributes(
    const ::istio::mixer::v1::config::client::TransportConfig &transport,
    std::string *serialized_forward_attributes) {
  if (!transport.attributes_for_mixer_proxy().attributes().empty()) {
    transport.attributes_for_mixer_proxy().SerializeToString(
        serialized_forward_attributes);
  }
}

Grpc::AsyncClientFactoryPtr GrpcClientFactoryForCluster(
    const std::string &cluster_name, Upstream::ClusterManager &cm,
    Stats::Scope &scope) {
  envoy::api::v2::core::GrpcService service;
  service.mutable_envoy_grpc()->set_cluster_name(cluster_name);

  // Workaround for https://github.com/envoyproxy/envoy/issues/2762
  UNREFERENCED_PARAMETER(scope);
  return std::make_unique<EnvoyGrpcAsyncClientFactory>(cm, service);
}

}  // namespace Utils
}  // namespace Envoy
