// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/grpc/transcoding/message_stream.h"

#include <memory>
#include <string>

#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/io/zero_copy_stream_impl_lite.h"

namespace google {
namespace api_manager {

namespace transcoding {

namespace pbio = ::google::protobuf::io;

namespace {

// a ZeroCopyInputStream implementation over a MessageStream implementation
class ZeroCopyStreamOverMessageStream : public pbio::ZeroCopyInputStream {
 public:
  // src - the underlying MessageStream. ZeroCopyStreamOverMessageStream doesn't
  //       maintain the ownership of src, the caller must make sure it exists
  //       throughtout the lifetime of ZeroCopyStreamOverMessageStream.
  ZeroCopyStreamOverMessageStream(MessageStream* src)
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

  ::google::protobuf::int64 ByteCount() const {
    // NOTE: we are changing the ByteCount() interpretation. In our case
    // ByteCount() returns the number of bytes available for reading at this
    // moment. In the original interpretation it is supposed to be the number
    // of bytes read so far.
    // We need this such that the consumers are able to read the gRPC delimited
    // message stream only if there is a full message available.
    if (position_ >= message_.size()) {
      // If the current message is all done, try to read the next message
      // to make sure we return the correct byte count.
      const_cast<ZeroCopyStreamOverMessageStream*>(this)->ReadNextMessage();
    }
    return static_cast<::google::protobuf::int64>(message_.size() - position_);
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

std::unique_ptr<::google::protobuf::io::ZeroCopyInputStream>
MessageStream::CreateZeroCopyInputStream() {
  return std::unique_ptr<::google::protobuf::io::ZeroCopyInputStream>(
      new ZeroCopyStreamOverMessageStream(this));
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
