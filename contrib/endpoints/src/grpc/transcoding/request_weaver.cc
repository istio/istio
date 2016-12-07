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
#include "contrib/endpoints/src/grpc/transcoding/request_weaver.h"

#include <string>
#include <vector>

#include "google/protobuf/stubs/stringpiece.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/internal/datapiece.h"
#include "google/protobuf/util/internal/object_writer.h"

namespace google {
namespace api_manager {

namespace transcoding {

namespace pb = google::protobuf;
namespace pbconv = google::protobuf::util::converter;

RequestWeaver::RequestWeaver(std::vector<BindingInfo> bindings,
                             pbconv::ObjectWriter* ow)
    : root_(), current_(), ow_(ow), non_actionable_depth_(0) {
  for (const auto& b : bindings) {
    Bind(std::move(b.field_path), std::move(b.value));
  }
}

RequestWeaver* RequestWeaver::StartObject(pb::StringPiece name) {
  ow_->StartObject(name);
  if (current_.empty()) {
    // The outermost StartObject("");
    current_.push(&root_);
    return this;
  }
  if (non_actionable_depth_ == 0) {
    WeaveInfo* info = current_.top()->FindWeaveMsg(name);
    if (info != nullptr) {
      current_.push(info);
      return this;
    }
  }
  // At this point, we don't match any messages we need to weave into, so
  // we won't need to do any matching until we leave this object.
  ++non_actionable_depth_;
  return this;
}

RequestWeaver* RequestWeaver::EndObject() {
  if (non_actionable_depth_ > 0) {
    --non_actionable_depth_;
  } else {
    WeaveTree(current_.top());
    current_.pop();
  }
  ow_->EndObject();
  return this;
}

RequestWeaver* RequestWeaver::StartList(google::protobuf::StringPiece name) {
  ow_->StartList(name);
  // We don't support weaving inside lists, so we won't need to do any matching
  // until we leave this list.
  ++non_actionable_depth_;
  return this;
}

RequestWeaver* RequestWeaver::EndList() {
  ow_->EndList();
  --non_actionable_depth_;
  return this;
}

RequestWeaver* RequestWeaver::RenderBool(google::protobuf::StringPiece name,
                                         bool value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderBool(name, value);
  return this;
}

RequestWeaver* RequestWeaver::RenderInt32(google::protobuf::StringPiece name,
                                          google::protobuf::int32 value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderInt32(name, value);
  return this;
}

RequestWeaver* RequestWeaver::RenderUint32(google::protobuf::StringPiece name,
                                           google::protobuf::uint32 value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderUint32(name, value);
  return this;
}

RequestWeaver* RequestWeaver::RenderInt64(google::protobuf::StringPiece name,
                                          google::protobuf::int64 value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderInt64(name, value);
  return this;
}

RequestWeaver* RequestWeaver::RenderUint64(google::protobuf::StringPiece name,
                                           google::protobuf::uint64 value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderUint64(name, value);
  return this;
}

RequestWeaver* RequestWeaver::RenderDouble(google::protobuf::StringPiece name,
                                           double value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderDouble(name, value);
  return this;
}

RequestWeaver* RequestWeaver::RenderFloat(google::protobuf::StringPiece name,
                                          float value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderFloat(name, value);
  return this;
}

RequestWeaver* RequestWeaver::RenderString(
    google::protobuf::StringPiece name, google::protobuf::StringPiece value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderString(name, value);
  return this;
}

RequestWeaver* RequestWeaver::RenderNull(google::protobuf::StringPiece name) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderNull(name);
  return this;
}

RequestWeaver* RequestWeaver::RenderBytes(google::protobuf::StringPiece name,
                                          google::protobuf::StringPiece value) {
  if (non_actionable_depth_ == 0) {
    CollisionCheck(name);
  }
  ow_->RenderBytes(name, value);
  return this;
}

void RequestWeaver::Bind(std::vector<const pb::Field*> field_path,
                         std::string value) {
  WeaveInfo* current = &root_;

  // Find or create the path from the root to the leaf message, where the value
  // should be injected.
  for (size_t i = 0; i < field_path.size() - 1; ++i) {
    current = current->FindOrCreateWeaveMsg(field_path[i]);
  }

  if (!field_path.empty()) {
    current->bindings.emplace_back(field_path.back(), std::move(value));
  }
}

void RequestWeaver::WeaveTree(RequestWeaver::WeaveInfo* info) {
  for (const auto& data : info->bindings) {
    pbconv::ObjectWriter::RenderDataPieceTo(
        pbconv::DataPiece(pb::StringPiece(data.second), true),
        pb::StringPiece(data.first->name()), ow_);
  }
  info->bindings.clear();
  for (auto& msg : info->messages) {
    // Enter into the message only if there are bindings or submessages left
    if (!msg.second.bindings.empty() || !msg.second.messages.empty()) {
      ow_->StartObject(msg.first->name());
      WeaveTree(&msg.second);
      ow_->EndObject();
    }
  }
  info->messages.clear();
}

void RequestWeaver::CollisionCheck(pb::StringPiece name) {
  if (current_.empty()) return;

  for (auto it = current_.top()->bindings.begin();
       it != current_.top()->bindings.end();) {
    if (name == it->first->name()) {
      if (it->first->cardinality() == pb::Field::CARDINALITY_REPEATED) {
        pbconv::ObjectWriter::RenderDataPieceTo(
            pbconv::DataPiece(pb::StringPiece(it->second), true), name, ow_);
      } else {
        // TODO: Report collision error. For now we just ignore
        // the conflicting binding.
      }
      it = current_.top()->bindings.erase(it);
      continue;
    }
    ++it;
  }
}

RequestWeaver::WeaveInfo* RequestWeaver::WeaveInfo::FindWeaveMsg(
    const pb::StringPiece field_name) {
  for (auto& msg : messages) {
    if (field_name == msg.first->name()) {
      return &msg.second;
    }
  }
  return nullptr;
}

RequestWeaver::WeaveInfo* RequestWeaver::WeaveInfo::CreateWeaveMsg(
    const pb::Field* field) {
  messages.emplace_back(field, WeaveInfo());
  return &messages.back().second;
}

RequestWeaver::WeaveInfo* RequestWeaver::WeaveInfo::FindOrCreateWeaveMsg(
    const pb::Field* field) {
  WeaveInfo* found = FindWeaveMsg(field->name());
  return found == nullptr ? CreateWeaveMsg(field) : found;
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
