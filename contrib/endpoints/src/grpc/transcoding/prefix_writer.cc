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
#include "src/grpc/transcoding/prefix_writer.h"

#include <string>

#include "google/protobuf/stubs/stringpiece.h"
#include "google/protobuf/stubs/strutil.h"
#include "google/protobuf/util/internal/object_writer.h"

namespace google {
namespace api_manager {

namespace transcoding {

PrefixWriter::PrefixWriter(const std::string& prefix,
                           google::protobuf::util::converter::ObjectWriter* ow)
    : prefix_(google::protobuf::Split(prefix, ".", true)),
      non_actionable_depth_(0),
      writer_(ow) {}

PrefixWriter* PrefixWriter::StartObject(google::protobuf::StringPiece name) {
  if (++non_actionable_depth_ == 1) {
    name = StartPrefix(name);
  }
  writer_->StartObject(name);
  return this;
}

PrefixWriter* PrefixWriter::EndObject() {
  writer_->EndObject();
  if (--non_actionable_depth_ == 0) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::StartList(google::protobuf::StringPiece name) {
  if (++non_actionable_depth_ == 1) {
    name = StartPrefix(name);
  }
  writer_->StartList(name);
  return this;
}

PrefixWriter* PrefixWriter::EndList() {
  writer_->EndList();
  if (--non_actionable_depth_ == 0) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderBool(google::protobuf::StringPiece name,
                                       bool value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderBool(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderInt32(google::protobuf::StringPiece name,
                                        google::protobuf::int32 value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderInt32(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderUint32(google::protobuf::StringPiece name,
                                         google::protobuf::uint32 value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderUint32(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderInt64(google::protobuf::StringPiece name,
                                        google::protobuf::int64 value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderInt64(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderUint64(google::protobuf::StringPiece name,
                                         google::protobuf::uint64 value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderUint64(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderDouble(google::protobuf::StringPiece name,
                                         double value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderDouble(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderFloat(google::protobuf::StringPiece name,
                                        float value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderFloat(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderString(google::protobuf::StringPiece name,
                                         google::protobuf::StringPiece value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderString(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderBytes(google::protobuf::StringPiece name,
                                        google::protobuf::StringPiece value) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }
  writer_->RenderBytes(name, value);
  if (root) {
    EndPrefix();
  }
  return this;
}

PrefixWriter* PrefixWriter::RenderNull(google::protobuf::StringPiece name) {
  bool root = non_actionable_depth_ == 0;
  if (root) {
    name = StartPrefix(name);
  }

  writer_->RenderNull(name);
  if (root) {
    EndPrefix();
  }
  return this;
}

google::protobuf::StringPiece PrefixWriter::StartPrefix(
    google::protobuf::StringPiece name) {
  for (const auto& prefix : prefix_) {
    writer_->StartObject(name);
    name = prefix;
  }
  return name;
}

void PrefixWriter::EndPrefix() {
  for (size_t i = 0; i < prefix_.size(); ++i) {
    writer_->EndObject();
  }
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
