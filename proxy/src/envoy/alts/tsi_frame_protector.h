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

#include "common/common/c_smart_ptr.h"
#include "envoy/buffer/buffer.h"
#include "envoy/event/dispatcher.h"

#include "proxy/src/envoy/alts/transport_security_interface_wrapper.h"

namespace Envoy {
namespace Security {

typedef CSmartPtr<tsi_frame_protector, tsi_frame_protector_destroy>
    CFrameProtectorPtr;

/**
 * A C++ wrapper for tsi_frame_protector interface.
 * For detail of tsi_frame_protector, see
 * https://github.com/grpc/grpc/blob/v1.10.0/src/core/tsi/transport_security_interface.h#L70
 *
 * TODO(lizan): migrate to tsi_zero_copy_grpc_protector for further optimization
 */
class TsiFrameProtector final {
 public:
  explicit TsiFrameProtector(tsi_frame_protector* frame_protector);

  /**
   * Wrapper for tsi_frame_protector_protect
   * @param input supplies the input buffer, the method will drain it when it is
   * protected.
   * @param output supplies the output buffer
   * @return tsi_result the status.
   */
  tsi_result protect(Buffer::Instance& input, Buffer::Instance& output);

  /**
   * Wrapper for tsi_frame_protector_unprotect
   * @param input supplies the input buffer, the method will drain it when it is
   * protected.
   * @param output supplies the output buffer
   * @return tsi_result the status.
   */
  tsi_result unprotect(Buffer::Instance& input, Buffer::Instance& output);

 private:
  CFrameProtectorPtr frame_protector_;
};

typedef std::unique_ptr<TsiFrameProtector> TsiFrameProtectorPtr;

}  // namespace Security
}  // namespace Envoy
