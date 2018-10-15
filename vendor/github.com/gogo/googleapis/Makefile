URL="https://raw.githubusercontent.com/googleapis/googleapis/master/"

regenerate:
	go install ./protoc-gen-gogogoogleapis

	protoc \
	--gogogoogleapis_out=\
	Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,\
	Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,\
	:. \
	-I=. \
	google/rpc/status.proto \
	google/rpc/error_details.proto \
	google/rpc/code.proto \

	protoc \
	--gogogoogleapis_out=\
	Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,\
	:. \
	-I=. \
	google/api/http.proto \
	google/api/annotations.proto

update:
	go install ./vendor/github.com/gogo/protobuf/gogoreplace

	(cd ./google/rpc && rm status.proto; wget ${URL}/google/rpc/status.proto)
	gogoreplace \
		'option go_package = "google.golang.org/genproto/googleapis/rpc/status;status";' \
		'option go_package = "rpc";' \
		./google/rpc/status.proto

	(cd ./google/rpc && rm error_details.proto; wget ${URL}/google/rpc/error_details.proto)
	gogoreplace \
		'option go_package = "google.golang.org/genproto/googleapis/rpc/errdetails;errdetails";' \
		'option go_package = "rpc";' \
		./google/rpc/error_details.proto

	(cd ./google/rpc && rm code.proto; wget ${URL}/google/rpc/code.proto)
	gogoreplace \
		'option go_package = "google.golang.org/genproto/googleapis/rpc/code;code";' \
		'option go_package = "rpc";' \
		./google/rpc/code.proto

	(cd ./google/api && rm http.proto; wget ${URL}/google/api/http.proto)
	gogoreplace \
		'option go_package = "google.golang.org/genproto/googleapis/api/annotations;annotations";' \
		'option go_package = "api";' \
		./google/api/http.proto

	(cd ./google/api && rm annotations.proto; wget ${URL}/google/api/annotations.proto)
	gogoreplace \
		'option go_package = "google.golang.org/genproto/googleapis/api/annotations;annotations";' \
		'option go_package = "api";' \
		./google/api/annotations.proto
