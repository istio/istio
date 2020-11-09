package authenticate

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestExtractBearerToken(t *testing.T) {
	testCases := map[string]struct {
		metadata                 metadata.MD
		expectedToken            string
		extractBearerTokenErrMsg string
	}{
		"No metadata": {
			expectedToken:            "",
			extractBearerTokenErrMsg: "no metadata is attached",
		},
		"No auth header": {
			metadata: metadata.MD{
				"random": []string{},
			},
			expectedToken:            "",
			extractBearerTokenErrMsg: "no HTTP authorization header exists",
		},
		"No bearer token": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
				},
			},
			expectedToken:            "",
			extractBearerTokenErrMsg: "no bearer token exists in HTTP authorization header",
		},
		"With bearer token": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
					"Bearer bearer-token",
				},
			},
			expectedToken: "bearer-token",
		},
	}

	for id, tc := range testCases {
		ctx := context.Background()
		if tc.metadata != nil {
			ctx = metadata.NewIncomingContext(ctx, tc.metadata)
		}

		actual, err := ExtractBearerToken(ctx)
		if len(tc.extractBearerTokenErrMsg) > 0 {
			if err == nil {
				t.Errorf("Case %s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.extractBearerTokenErrMsg {
				t.Errorf("Case %s: Incorrect error message: %s VS %s",
					id, err.Error(), tc.extractBearerTokenErrMsg)
			}
			continue
		} else if err != nil {
			t.Fatalf("Case %s: Unexpected Error: %v", id, err)
		}

		if actual != tc.expectedToken {
			t.Errorf("Case %q: Unexpected token: want %s but got %s", id, tc.expectedToken, actual)
		}
	}
}
