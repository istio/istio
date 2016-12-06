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
#include "src/grpc/transcoding/message_reader.h"

#include <memory>

#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/io/zero_copy_stream_impl.h"

namespace google {
namespace api_manager {

namespace transcoding {

namespace pb = ::google::protobuf;
namespace pbio = ::google::protobuf::io;

MessageReader::MessageReader(pbio::ZeroCopyInputStream* in)
    : in_(in),
      current_message_size_(0),
      have_current_message_size_(false),
      finished_(false) {}

namespace {

// A helper function that reads the given number of bytes from a
// ZeroCopyInputStream and copies it to the given buffer
bool ReadStream(pbio::ZeroCopyInputStream* stream, unsigned char* buffer,
                int size) {
  int size_in = 0;
  const void* data_in = nullptr;
  // While we have bytes to read
  while (size > 0) {
    if (!stream->Next(&data_in, &size_in)) {
      return false;
    }
    int to_copy = std::min(size, size_in);
    memcpy(buffer, data_in, to_copy);
    // Advance buffer and subtract the size to reflect the number of bytes left
    buffer += to_copy;
    size -= to_copy;
    // Keep track of uncopied bytes
    size_in -= to_copy;
  }
  // Return the uncopied bytes
  stream->BackUp(size_in);
  return true;
}

// Determines whether the stream is finished or not.
bool IsStreamFinished(pbio::ZeroCopyInputStream* stream) {
  int size = 0;
  const void* data = nullptr;
  if (!stream->Next(&data, &size)) {
    return true;
  } else {
    stream->BackUp(size);
    return false;
  }
}

// A helper function to extract the size from a gRPC wire format message
// delimiter - see http://www.grpc.io/docs/guides/wire.html.
unsigned DelimiterToSize(const unsigned char* delimiter) {
  unsigned size = 0;
  // Bytes 1-4 are big-endian 32-bit message size
  size = size | static_cast<unsigned>(delimiter[1]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[2]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[3]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[4]);
  return size;
}

}  // namespace

std::unique_ptr<pbio::ZeroCopyInputStream> MessageReader::NextMessage() {
  if (finished_) {
    // The stream has ended
    return std::unique_ptr<pbio::ZeroCopyInputStream>();
  }

  // Check if we have the current message size. If not try to read it.
  if (!have_current_message_size_) {
    const size_t kDelimiterSize = 5;
    if (in_->ByteCount() < static_cast<pb::int64>(kDelimiterSize)) {
      // We don't have 5 bytes available to read the length of the message.
      // Find out whether the stream is finished and return false.
      finished_ = IsStreamFinished(in_);
      return std::unique_ptr<pbio::ZeroCopyInputStream>();
    }

    // Try to read the delimiter
    unsigned char delimiter[kDelimiterSize] = {0};
    if (!ReadStream(in_, delimiter, sizeof(delimiter))) {
      finished_ = true;
      return std::unique_ptr<pbio::ZeroCopyInputStream>();
    }

    current_message_size_ = DelimiterToSize(delimiter);
    have_current_message_size_ = true;
  }

  // We interpret ZeroCopyInputStream::ByteCount() as the number of bytes
  // available for reading at the moment. Check if we have the full message
  // available to read.
  if (in_->ByteCount() < static_cast<pb::int64>(current_message_size_)) {
    // We don't have a full message
    return std::unique_ptr<pbio::ZeroCopyInputStream>();
  }

  // We have a message! Use LimitingInputStream to wrap the input stream and
  // limit it to current_message_size_ bytes to cover only the current message.
  auto result = std::unique_ptr<pbio::ZeroCopyInputStream>(
      new pbio::LimitingInputStream(in_, current_message_size_));

  // Reset the have_current_message_size_ for the next message
  have_current_message_size_ = false;

  return result;
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
