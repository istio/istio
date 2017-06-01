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
#include "src/envoy/mixer/grpc_transport.h"

#include "common/grpc/rpc_channel_impl.h"

using ::google::protobuf::util::Status;
using StatusCode = ::google::protobuf::util::error::Code;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// gRPC request timeout
const std::chrono::milliseconds kGrpcRequestTimeoutMs(5000);

// The name for the mixer server cluster.
const char* kMixerServerClusterName = "mixer_server";

// A thread local dispatcher.
thread_local Event::Dispatcher* thread_dispatcher = nullptr;

void thread_dispatcher_post(std::function<void()> fn) {
  ASSERT(thread_dispatcher);
  thread_dispatcher->post(fn);
}

}  // namespace

void GrpcTransport::SetDispatcher(Event::Dispatcher& dispatcher) {
  if (!thread_dispatcher) {
    thread_dispatcher = &dispatcher;
  }
}

GrpcTransport::GrpcTransport(Upstream::ClusterManager& cm)
    : channel_(NewChannel(cm)), stub_(channel_.get()) {}

void GrpcTransport::onSuccess() {
  log().debug("grpc: return OK");
  on_done_(Status::OK);
  // RpcChannelImpl object expects its OnComplete() is called before
  // deleted.  OnCompleted() is called after onSuccess()
  // Use the dispatch post to delay the deletion.
  thread_dispatcher_post([this]() { delete this; });
}

void GrpcTransport::onFailure(const Optional<uint64_t>& grpc_status,
                              const std::string& message) {
  // Envoy source/common/grpc/common.cc line 92
  // return invalid grpc_status and "non-200 response code" message
  // when failed to connect to grpc server.
  int code;
  if (!grpc_status.valid() && message == "non-200 response code") {
    code = StatusCode::UNAVAILABLE;
  } else {
    code = grpc_status.valid() ? grpc_status.value() : StatusCode::UNKNOWN;
  }
  log().debug("grpc failure: return {}, error {}", code, message);
  on_done_(Status(static_cast<StatusCode>(code),
                  ::google::protobuf::StringPiece(message)));
  thread_dispatcher_post([this]() { delete this; });
}

Grpc::RpcChannelPtr GrpcTransport::NewChannel(Upstream::ClusterManager& cm) {
  return Grpc::RpcChannelPtr(new Grpc::RpcChannelImpl(
      cm, kMixerServerClusterName, *this,
      Optional<std::chrono::milliseconds>(kGrpcRequestTimeoutMs)));
}

bool GrpcTransport::IsMixerServerConfigured(Upstream::ClusterManager& cm) {
  return cm.get(kMixerServerClusterName) != nullptr;
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
