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
#include "src/envoy/mixer/thread_dispatcher.h"

#include "common/common/enum_to_int.h"
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

// HTTP trace headers that should pass to gRPC metadata from origin request.
// x-request-id is added for easy debugging.
const LowerCaseString kRequestId("x-request-id");
const LowerCaseString kB3TraceId("x-b3-traceid");
const LowerCaseString kB3SpanId("x-b3-spanid");
const LowerCaseString kB3ParentSpanId("x-b3-parentspanid");
const LowerCaseString kB3Sampled("x-b3-sampled");
const LowerCaseString kB3Flags("x-b3-flags");
const LowerCaseString kOtSpanContext("x-ot-span-context");

inline void CopyHeaderEntry(const HeaderEntry* entry,
                            const LowerCaseString& key,
                            Http::HeaderMap& headers) {
  if (entry) {
    std::string val(entry->value().c_str(), entry->value().size());
    headers.addReferenceKey(key, val);
  }
}

}  // namespace

GrpcTransport::GrpcTransport(Upstream::ClusterManager& cm,
                             const HeaderMap* headers)
    : channel_(NewChannel(cm)), stub_(channel_.get()), headers_(headers) {}

void GrpcTransport::onSuccess() {
  log().debug("grpc: return OK");
  on_done_(Status::OK);
  // RpcChannelImpl object expects its OnComplete() is called before
  // deleted.  OnCompleted() is called after onSuccess()
  // Use the dispatch post to delay the deletion.
  GetThreadDispatcher().post([this]() { delete this; });
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
    code = grpc_status.valid() ? grpc_status.value()
                               : enumToInt(StatusCode::UNKNOWN);
  }
  log().debug("grpc failure: return {}, error {}", code, message);
  on_done_(Status(static_cast<StatusCode>(code),
                  ::google::protobuf::StringPiece(message)));
  GetThreadDispatcher().post([this]() { delete this; });
}

Grpc::RpcChannelPtr GrpcTransport::NewChannel(Upstream::ClusterManager& cm) {
  return Grpc::RpcChannelPtr(new Grpc::RpcChannelImpl(
      cm, kMixerServerClusterName, *this,
      Optional<std::chrono::milliseconds>(kGrpcRequestTimeoutMs)));
}

bool GrpcTransport::IsMixerServerConfigured(Upstream::ClusterManager& cm) {
  return cm.get(kMixerServerClusterName) != nullptr;
}

void GrpcTransport::onPreRequestCustomizeHeaders(Http::HeaderMap& headers) {
  if (!headers_) return;

  CopyHeaderEntry(headers_->RequestId(), kRequestId, headers);
  CopyHeaderEntry(headers_->XB3TraceId(), kB3TraceId, headers);
  CopyHeaderEntry(headers_->XB3SpanId(), kB3SpanId, headers);
  CopyHeaderEntry(headers_->XB3ParentSpanId(), kB3ParentSpanId, headers);
  CopyHeaderEntry(headers_->XB3Sampled(), kB3Sampled, headers);
  CopyHeaderEntry(headers_->XB3Flags(), kB3Flags, headers);

  // This one is NOT inline, need to do linar search.
  CopyHeaderEntry(headers_->get(kOtSpanContext), kOtSpanContext, headers);
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
