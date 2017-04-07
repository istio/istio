/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef GRPC_TRANSCODING_TRANSCODER_INPUT_STREAM_H_
#define GRPC_TRANSCODING_TRANSCODER_INPUT_STREAM_H_

#include "google/protobuf/io/zero_copy_stream.h"

namespace google {
namespace api_manager {
namespace transcoding {

class TranscoderInputStream
    : public virtual google::protobuf::io::ZeroCopyInputStream {
 public:
  // returns the number of bytes available to read at the moment.
  virtual int64_t BytesAvailable() const = 0;
};

}  // namespace transcoding
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODER_H_
