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
#include "contrib/endpoints/src/grpc/transcoding/json_request_translator.h"

#include <memory>
#include <string>
#include <vector>

#include "contrib/endpoints/src/grpc/transcoding/bookstore.pb.h"
#include "contrib/endpoints/src/grpc/transcoding/proto_stream_tester.h"
#include "contrib/endpoints/src/grpc/transcoding/request_translator_test_base.h"
#include "contrib/endpoints/src/grpc/transcoding/test_common.h"
#include "google/protobuf/io/zero_copy_stream.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {
namespace {

namespace pberr = google::protobuf::util::error;

// TranslationTestCase helps us build translation test cases and validate the
// translation output.
//
// The tests construct the test case using AddMessage() and Build(). With
// AddMessage() the JSON message as well as the expected proto are specified.
// TranslationTestCase builds the input JSON and for each expected proto message
// remembers the position in the input JSON where it's expected.
//
// After building the test case, the tests can use TestAt() method to validate
// the output translation stream (using the given ProtoStreamTester) at the
// specified position in JSON input.
class TranslationTestCase {
 public:
  TranslationTestCase(bool streaming)
      : input_json_(streaming ? "[" : ""), streaming_(streaming) {}

  bool Streaming() const { return streaming_; }

  // Add an input message.
  void AddMessage(const std::string& json, const std::string& expected_proto) {
    input_json_ += json;
    // The message expected_proto is expected at position input_json_.size()
    expected_.emplace_back(ExpectedAt{input_json_.size(), expected_proto});
    if (streaming_) {
      input_json_ += ", ";
    }
  }

  // Build the test case.
  void Build() {
    if (streaming_) {
      input_json_ += "]";
    }
    Reset();
  }

  // Resets the test case to start (re-)testing
  void Reset() { next_expected_ = std::begin(expected_); }

  // Return the input JSON to be passed to the translator.
  const std::string& InputJson() const { return input_json_; }

  // Test the output translation stream (using the specified ProtoStreamTester),
  // after [0, pos) part of the input JSON has been fed to the translator.
  template <typename MessageType>
  bool TestAt(ProtoStreamTester& tester, size_t pos) {
    // While we have proto messages that are expected before or at pos, try to
    // match them.
    while (next_expected_ != std::end(expected_) && pos >= next_expected_->at) {
      // Must not be finished
      if (!tester.ExpectFinishedEq(false)) {
        ADD_FAILURE() << "Finished unexpectedly at "
                      << input_json_.substr(0, pos) << std::endl;
        return false;
      }
      // Match the message
      if (!tester.ExpectNextEq<MessageType>(next_expected_->proto)) {
        ADD_FAILURE() << "Failed matching the message at "
                      << input_json_.substr(0, pos) << std::endl;
        return false;
      }
      ++next_expected_;
    }
    if (!tester.ExpectNone()) {
      // We have processed all expected messages, the stream must not have
      // any messages left.
      ADD_FAILURE() << "Unexpected message at " << input_json_.substr(0, pos)
                    << std::endl;
      return false;
    }
    if (pos < input_json_.size()) {
      // There is still input left, so the stream must not be finished yet.
      if (!tester.ExpectFinishedEq(false)) {
        ADD_FAILURE() << "Finished unexpectedly at "
                      << input_json_.substr(0, pos) << std::endl;
        return false;
      }
    } else {  // pos >= input_json_.size()
      // There is no input left, so the stream must be finished.
      if (!tester.ExpectFinishedEq(true)) {
        ADD_FAILURE() << "Not finished after all input has been translated\n";
        return false;
      }
    }
    return true;
  }

 private:
  std::string input_json_;
  bool streaming_;

  struct ExpectedAt {
    // The position in the input_json_, after which this proto is expected
    size_t at;
    // The expected proto message in a proto-text format
    std::string proto;
  };
  std::vector<ExpectedAt> expected_;

  // An iterator to the next expected message.
  std::vector<ExpectedAt>::const_iterator next_expected_;
};

class JsonRequestTranslatorTest : public RequestTranslatorTestBase {
 protected:
  JsonRequestTranslatorTest() : streaming_(false) {}

  // Sets whether this is a streaming call or not. Use it before calling
  // Build(). Default is non-streaming
  void SetStreaming(bool streaming) { streaming_ = streaming; }

  // Add an input chunk
  void AddChunk(const std::string& json) { input_->AddChunk(json); }

  // End the input
  void Finish() { input_->Finish(); }

  // Test the translation test case with different partitions of the input
  // chunk_count - the number of chunks (parts) per partition
  // partitioning_coefficient - defines how exhaustive the test should be. See
  //                            the comment on RunTestForInputPartitions() in
  //                            test_common.h for more details.
  // test_case - the test case to run,
  // delimiters - whether to output GRPC delimiters when translating or not.
  template <typename MessageType>
  bool RunTest(size_t chunk_count, double partitioning_coefficient,
               TranslationTestCase* test_case, bool delimiters) {
    // Set streaming flag
    SetStreaming(test_case->Streaming());
    // Run the test for different m-partitions of the input JSON.
    return RunTestForInputPartitions(
        chunk_count, partitioning_coefficient, test_case->InputJson(),
        [this, test_case](const std::vector<size_t>& t) {
          // Rebuild the translator & reset the test case to reset its state.
          Build();
          test_case->Reset();

          // Feed the chunks according to the partition defined by tuple t and
          // test along the way.
          const std::string& input = test_case->InputJson();
          size_t pos = 0;
          for (size_t i = 0; i < t.size(); ++i) {
            AddChunk(input.substr(pos, t[i] - pos));
            pos = t[i];
            if (!test_case->TestAt<MessageType>(Tester(), pos)) {
              return false;
            }
          }
          // Feed the last chunk, finish & test.
          AddChunk(input.substr(pos));
          Finish();
          return test_case->TestAt<MessageType>(Tester(), input.size());
        });
  }

  // Calls the above function both with delimiters=true and delimiters=false.
  template <typename MessageType>
  bool RunTest(size_t chunk_count, double partitioning_coefficient,
               TranslationTestCase* test_case) {
    return RunTest<MessageType>(chunk_count, partitioning_coefficient,
                                test_case, false) &&
           RunTest<MessageType>(chunk_count, partitioning_coefficient,
                                test_case, true);
  }

 private:
  // RequestTranslatorTestBase::Create()
  virtual MessageStream* Create(
      google::protobuf::util::TypeResolver& type_resolver, bool delimiters,
      RequestInfo request_info) {
    input_.reset(new TestZeroCopyInputStream());
    translator_.reset(new JsonRequestTranslator(&type_resolver, input_.get(),
                                                std::move(request_info),
                                                streaming_, delimiters));
    return &translator_->Output();
  }

  bool streaming_;
  std::unique_ptr<TestZeroCopyInputStream> input_;
  std::unique_ptr<JsonRequestTranslator> translator_;
};

TEST_F(JsonRequestTranslatorTest, Simple) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  TranslationTestCase tc(false);
  tc.AddMessage(R"({ "name" : "1", "theme" : "Russian" })",
                R"(name : "1" theme : "Russian")");
  tc.Build();

  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(2, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(3, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(4, 0.5, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(5, 0.4, &tc)));
}

TEST_F(JsonRequestTranslatorTest, Nested) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Book");
  TranslationTestCase tc(false);
  tc.AddMessage(
      R"({
          "name" : "8",
          "author" : "Leo Tolstoy",
          "title" : "War and Peace",
          "authorInfo" : {
            "firstName" : "Leo",
            "lastName" : "Tolstoy",
            "bio" : {
              "yearBorn" : 1830,
              "yearDied" : 1910,
              "text" : "some text"
            }
          }
        })",
      R"(
          name : "8"
          author : "Leo Tolstoy"
          title : "War and Peace"
          author_info {
            first_name : "Leo"
            last_name : "Tolstoy"
            bio {
              year_born : 1830
              year_died : 1910
              text : "some text"
            }
          }
        )");
  tc.Build();

  EXPECT_TRUE((RunTest<Book>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Book>(2, 0.5, &tc)));
  EXPECT_TRUE((RunTest<Book>(3, 0.05, &tc)));
}

TEST_F(JsonRequestTranslatorTest, Prefix) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetBodyPrefix("book");
  TranslationTestCase tc(false);
  tc.AddMessage(
      R"({
          "name" : "9",
          "author" : "Leo Tolstoy",
          "title" : "War and Peace",
        })",
      R"(
          book {
            name : "9"
            author : "Leo Tolstoy"
            title : "War and Peace"
          }
        )");
  tc.Build();

  EXPECT_TRUE((RunTest<CreateBookRequest>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<CreateBookRequest>(2, 1.0, &tc)));
  EXPECT_TRUE((RunTest<CreateBookRequest>(3, 0.1, &tc)));
}

TEST_F(JsonRequestTranslatorTest, Bindings) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetBodyPrefix("book");
  AddVariableBinding("book.authorInfo.firstName", "Leo");
  AddVariableBinding("book.authorInfo.lastName", "Tolstoy");
  TranslationTestCase tc(false);
  tc.AddMessage(
      R"({
          "name" : "11",
          "author" : "Leo Tolstoy",
          "title" : "Anna Karenina",
        })",
      R"(
          book {
            name : "11"
            author : "Leo Tolstoy"
            title : "Anna Karenina"
            author_info : {
              first_name : "Leo"
              last_name : "Tolstoy"
            }
          }
        )");
  tc.Build();

  EXPECT_TRUE((RunTest<CreateBookRequest>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<CreateBookRequest>(2, 1.0, &tc)));
  EXPECT_TRUE((RunTest<CreateBookRequest>(3, 0.1, &tc)));
}

TEST_F(JsonRequestTranslatorTest, MorePrefixAndBindings) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetBodyPrefix("book.authorInfo.bio");
  AddVariableBinding("book.name", "7");
  AddVariableBinding("book.author", "Fyodor Dostoevski");
  AddVariableBinding("book.title", "Idiot");
  AddVariableBinding("book.author_info.first_name", "Fyodor");
  AddVariableBinding("book.author_info.last_name", "Dostoevski");
  TranslationTestCase tc(false);
  tc.AddMessage(
      R"({
          "yearBorn" : "1840",
          "yearDied" : "1920",
          "text" : "bio text",
        })",
      R"(
          book {
            name : "7"
            author : "Fyodor Dostoevski"
            title : "Idiot"
            author_info : {
              first_name : "Fyodor"
              last_name : "Dostoevski"
              bio {
                year_born : 1840
                year_died : 1920
                text : "bio text"
              }
            }
          }
        )");
  tc.Build();

  EXPECT_TRUE((RunTest<CreateBookRequest>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<CreateBookRequest>(2, 1.0, &tc)));
  EXPECT_TRUE((RunTest<CreateBookRequest>(3, 0.1, &tc)));
}

TEST_F(JsonRequestTranslatorTest, OnlyBindings) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  AddVariableBinding("name", "1");
  AddVariableBinding("theme", "Fiction");
  TranslationTestCase tc(false);
  tc.AddMessage("", R"( name : "1" theme : "Fiction" )");
  tc.Build();

  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, ScalarBody) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  SetBodyPrefix("theme");
  TranslationTestCase tc(false);
  tc.AddMessage(R"("History")", R"(theme : "History")");
  tc.Build();

  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(2, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(3, 0.1, &tc)));
}

TEST_F(JsonRequestTranslatorTest, Empty) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();
  Finish();
  EXPECT_TRUE(Tester().ExpectFinishedEq(false));
  EXPECT_TRUE(Tester().ExpectNextEq<Shelf>(""));
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(JsonRequestTranslatorTest, Large) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");

  auto sizes = {1, 256, 1024, 1234, 4096, 65537};
  for (auto size : sizes) {
    auto theme = GenerateInput("0123456789abcdefgh", size);

    TranslationTestCase tc(false);
    tc.AddMessage(
        R"({ "name" : "1",  "theme" : ")" + theme + R"("})",
        R"(name : "1" theme : ")" + theme + R"(")");
    tc.Build();

    EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
  }
}

TEST_F(JsonRequestTranslatorTest, OneByteChunks) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  Build();

  std::string json = R"({ "name" : "99", "theme" : "Fiction" })";

  for (auto c : json) {
    EXPECT_TRUE(Tester().ExpectNone());
    EXPECT_TRUE(Tester().ExpectFinishedEq(false));
    AddChunk(std::string(1, c));
  }
  Finish();

  EXPECT_TRUE(Tester().ExpectFinishedEq(false));
  EXPECT_TRUE(Tester().ExpectNextEq<Shelf>(R"(name : "99" theme : "Fiction")"));
  EXPECT_TRUE(Tester().ExpectFinishedEq(true));
}

TEST_F(JsonRequestTranslatorTest, UnknownsIgnored) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  TranslationTestCase tc(false);
  tc.AddMessage(
      R"({
            "name" : "1",
            "theme" : "Russian",
            "unknownField" : "ignored",
            "unknownObject" : {
              "name" : "value"
            },
            "unknownArray" : [1, 2, 3, 4]
        })",
      R"(name : "1" theme : "Russian")");
  tc.Build();

  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(2, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, ErrorInvalidJson) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");

  auto invalids = {
      "Invalid",
      R"({"name")",
      R"({"name" : 1)",
      R"({"theme" , "name" : 1})",
      R"({"name" : 1])",
      R"(["theme" , "name" : 1})",
      R"({"name" : 1, { "a" : [ "c", "d"} ]})",
      R"(["name" : 1 ]})",
      R"([{"name" : 1 ]})",
      "  ",
      "\r  \t\n",
  };

  for (auto streaming : {false, true}) {
    for (auto invalid : invalids) {
      SetStreaming(streaming);
      Build();
      AddChunk(invalid);
      Finish();
      EXPECT_TRUE(Tester().ExpectNone());
      EXPECT_TRUE(Tester().ExpectStatusEq(pberr::INVALID_ARGUMENT));
    }
  }
}

TEST_F(JsonRequestTranslatorTest, StreamingSimple) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  TranslationTestCase tc(/*streaming*/ true);
  tc.AddMessage(R"({ "name" : "1", "theme" : "Russian" })",
                R"(name : "1" theme : "Russian")");
  tc.AddMessage(R"({ "name" : "2", "theme" : "History" })",
                R"(name : "2" theme : "History")");
  tc.AddMessage(R"({ "name" : "3", "theme" : "Mistery" })",
                R"(name : "3" theme : "Mistery")");
  tc.Build();

  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(2, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(3, 0.2, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(4, 0.1, &tc)));
}

TEST_F(JsonRequestTranslatorTest, StreamingNested) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Book");
  TranslationTestCase tc(/*streaming*/ true);
  tc.AddMessage(
      R"({
          "name" : "1",
          "author" : "Leo Tolstoy",
          "title" : "War and Peace",
          "authorInfo" : {
            "firstName" : "Leo",
            "lastName" : "Tolstoy",
          }
        })",
      R"(
          name : "1"
          author : "Leo Tolstoy"
          title : "War and Peace"
          author_info {
            first_name : "Leo"
            last_name : "Tolstoy"
          }
        )");
  tc.AddMessage(
      R"({
          "name" : "2",
          "author" : "Leo Tolstoy",
          "title" : "Anna Karenina",
          "authorInfo" : {
            "firstName" : "Leo",
            "lastName" : "Tolstoy",
          }
        })",
      R"(
          name : "2"
          author : "Leo Tolstoy"
          title : "Anna Karenina"
          author_info {
            first_name : "Leo"
            last_name : "Tolstoy"
          }
        )");
  tc.AddMessage(
      R"({
          "name" : "3",
          "author" : "Fyodor Dostoevski",
          "title" : "Crime and Punishment",
          "authorInfo" : {
            "firstName" : "Fyodor",
            "lastName" : "Dostoevski",
          }
        })",
      R"(
          name : "3"
          author : "Fyodor Dostoevski"
          title : "Crime and Punishment"
          author_info {
            first_name : "Fyodor"
            last_name : "Dostoevski"
          }
        )");
  tc.Build();

  EXPECT_TRUE((RunTest<Book>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Book>(2, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, StreamingPrefixAndBindings) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("CreateBookRequest");
  SetBodyPrefix("book.authorInfo");
  AddVariableBinding("book.name", "100");
  AddVariableBinding("book.title", "The Old Man And The Sea");
  AddVariableBinding("book.author", "Ernest Hemingway");
  TranslationTestCase tc(/*streaming*/ true);
  tc.AddMessage(
      R"({
          "firstName" : "Ernest",
          "lastName" : "Hemingway",
        })",
      R"(
          book {
            name : "100"
            author : "Ernest Hemingway"
            title : "The Old Man And The Sea"
            author_info {
              first_name : "Ernest"
              last_name : "Hemingway"
            }
          }
        )");
  // Note that variable bindings apply only to the first message. So "name",
  // "title" and "author" won't be presend in 2nd and 3rd messages
  tc.AddMessage(
      R"({
          "firstName" : "Ernest-2",
          "lastName" : "Hemingway-2",
        })",
      R"(
          book {
            author_info {
              first_name : "Ernest-2"
              last_name : "Hemingway-2"
            }
          }
        )");
  tc.AddMessage(
      R"({
          "firstName" : "Ernest-3",
          "lastName" : "Hemingway-3",
        })",
      R"(
          book {
            author_info {
              first_name : "Ernest-3"
              last_name : "Hemingway-3"
            }
          }
        )");
  tc.Build();

  EXPECT_TRUE((RunTest<CreateBookRequest>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<CreateBookRequest>(2, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, Streaming1KMessages) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  TranslationTestCase tc(/*streaming*/ true);

  for (size_t i = 0; i < 1000; ++i) {
    std::string no = std::to_string(i + 1);
    tc.AddMessage(R"({ "name" : ")" + no + R"(", "theme" : "th)" + no + R"("
    })",
                  R"(name : ")" + no + R"(" theme : "th)" + no + R"(")");
  }
  tc.Build();

  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, StreamingScalars) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  SetBodyPrefix("theme");
  TranslationTestCase tc(/*streaming*/ true);
  // ["Classic", "Fiction", "Documentary"]
  tc.AddMessage(R"("Classic")", R"(theme : "Classic")");
  tc.AddMessage(R"("Fiction")", R"(theme : "Fiction")");
  tc.AddMessage(R"("Documentary")", R"(theme : "Documentary")");
  tc.Build();

  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<Shelf>(2, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, StreamingArrays) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("ListShelvesResponse");
  // "shelves" is a repeated field in "ListShelvesResponse"
  SetBodyPrefix("shelves");
  TranslationTestCase tc(/*streaming*/ true);
  // The input becomes an array of arrays:
  // [
  //    [ {shelf1}, {shelf2} ],
  //    [ {shelf3}, {shelf4} ],
  //    [ {shelf5}, {shelf6} ],
  // ]
  // The output is a stream of messages of type ListShelvesResponse:
  // 1 - "shelves {shelf1} shelves {shelf 2}"
  // 2 - "shelves {shelf3} shelves {shelf 4}"
  // 3 - "shelves {shelf5} shelves {shelf 6}"
  tc.AddMessage(
      R"([
        {"name" : "1", "theme" : "Classic"},
        {"name" : "2", "theme" : "Fiction"},
        {"name" : "3", "theme" : "Documentary"},
      ])",
      R"(
        shelves : { name : "1" theme : "Classic" }
        shelves : { name : "2" theme : "Fiction" }
        shelves : { name : "3" theme : "Documentary" }
      )");
  tc.AddMessage(
      R"([
        {"name" : "5", "theme" : "Drama"},
        {"name" : "6", "theme" : "Russian"},
      ])",
      R"(
        shelves : { name : "5" theme : "Drama" }
        shelves : { name : "6" theme : "Russian" }
      )");

  tc.Build();

  EXPECT_TRUE((RunTest<ListShelvesResponse>(1, 1.0, &tc)));
  EXPECT_TRUE((RunTest<ListShelvesResponse>(2, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, StreamingEmptyStream) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  TranslationTestCase tc(/*streaming*/ true);
  tc.Build();
  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, StreamingEmptyMessages) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  TranslationTestCase tc(/*streaming*/ true);
  tc.AddMessage("{}", "");
  tc.AddMessage(R"({"theme" : "Classic"})", R"(theme : "Classic")");
  tc.AddMessage("{}", "");
  tc.Build();
  EXPECT_TRUE((RunTest<Shelf>(1, 1.0, &tc)));
}

TEST_F(JsonRequestTranslatorTest, StreamingErrorNotAnArray) {
  LoadService("bookstore_service.pb.txt");
  SetMessageType("Shelf");
  SetStreaming(true);
  Build();
  AddChunk(R"({"name" : "1"})");
  Finish();
  EXPECT_TRUE(Tester().ExpectNone());
  EXPECT_TRUE(Tester().ExpectStatusEq(pberr::INVALID_ARGUMENT));
}

}  // namespace
}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
