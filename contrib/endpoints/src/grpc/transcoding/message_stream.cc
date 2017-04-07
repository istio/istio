// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/grpc/transcoding/message_stream.h"

#include <memory>
#include <string>

#include "google/protobuf/io/zero_copy_stream_impl_lite.h"

namespace google {
namespace api_manager {

namespace transcoding {

namespace pbio = ::google::protobuf::io;

namespace {

// a ZeroCopyInputStream implementation over a MessageStream implementation
class InputStreamOverMessageStream : public TranscoderInputStream {
 public:
  // src - the underlying MessageStream. InputStreamOverMessageStream doesn't
  //       maintain the ownership of src, the caller must make sure it exists
  //       throughtout the lifetime of InputStreamOverMessageStream.
  InputStreamOverMessageStream(MessageStream* src)
      : src_(src), message_(), position_(0) {}

  // ZeroCopyInputStream implementation
  bool Next(const void** data, int* size) {
    // Done with the current message, try to get another one.
    if (position_ >= message_.size()) {
      ReadNextMessage();
    }

    if (position_ < message_.size()) {
      *data = static_cast<const void*>(&message_[position_]);
      // Assuming message_.size() - position_ < INT_MAX
      *size = static_cast<int>(message_.size() - position_);
      // Advance the position
      position_ = message_.size();
      return true;
    } else {
      // No data at this point.
      *size = 0;
      // Return false if the source stream has finished as this is the end
      // of the data; otherwise return true.
      return !src_->Finished();
    }
  }

  void BackUp(int count) {
    if (count > 0 && static_cast<size_t>(count) <= position_) {
      position_ -= static_cast<size_t>(count);
    }
    // Otherwise, BackUp has been called illegaly, so we ignore it.
  }

  bool Skip(int) { return false; }  // Not implemented (no need)

  google::protobuf::int64 ByteCount() const { return 0; }  // Not implemented

  int64_t BytesAvailable() const {
    if (position_ >= message_.size()) {
      // If the current message is all done, try to read the next message
      // to make sure we return the correct byte count.
      const_cast<InputStreamOverMessageStream*>(this)->ReadNextMessage();
    }
    return static_cast<int64_t>(message_.size() - position_);
  }

 private:
  // Updates the current message and creates an ArrayInputStream over it.
  void ReadNextMessage() {
    message_.clear();
    position_ = 0;
    // Try to find the next non-empty message in the stream
    while (message_.empty() && src_->NextMessage(&message_)) {
    }
  }

  // The source MessageStream
  MessageStream* src_;

  // The current message being read
  std::string message_;

  // The current position in the current message
  size_t position_;
};

}  // namespace

std::unique_ptr<TranscoderInputStream> MessageStream::CreateInputStream() {
  return std::unique_ptr<TranscoderInputStream>(
      new InputStreamOverMessageStream(this));
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
