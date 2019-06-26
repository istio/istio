# jwt

JWT tokens

# SYNOPSIS

```go
package jwt_test

import (
  "bytes"
  "crypto/rand"
  "crypto/rsa"
  "encoding/json"
  "fmt"
  "time"

  "github.com/lestrrat-go/jwx/jwa"
  "github.com/lestrrat-go/jwx/jwt"
)

func ExampleSignAndParse() {
  privKey, err := rsa.GenerateKey(rand.Reader, 2048)
  if err != nil {
    fmt.Printf("failed to generate private key: %s\n", err)
    return
  }

  var payload []byte
  { // Create signed payload
    token := jwt.New()
    token.Set(`foo`, `bar`)
    payload, err = token.Sign(jwa.RS256, privKey)
    if err != nil {
      fmt.Printf("failed to generate signed payload: %s\n", err)
      return
    }
  }

  { // Parse signed payload
    // Use jwt.ParseVerify if you want to make absolutely sure that you
    // are going to verify the signatures every time
    token, err := jwt.Parse(bytes.NewReader(payload), jwt.WithVerify(jwa.RS256, &privKey.PublicKey))
    if err != nil {
      fmt.Printf("failed to parse JWT token: %s\n", err)
      return
    }
    buf, err := json.MarshalIndent(token, "", "  ")
    if err != nil {
      fmt.Printf("failed to generate JSON: %s\n", err)
      return
    }
    fmt.Printf("%s\n", buf)
  }
}

func ExampleToken() {
  t := jwt.New()
  t.Set(jwt.SubjectKey, `https://github.com/lestrrat-go/jwx/jwt`)
  t.Set(jwt.AudienceKey, `Golang Users`)
  t.Set(jwt.IssuedAtKey, time.Unix(aLongLongTimeAgo, 0))
  t.Set(`privateClaimKey`, `Hello, World!`)

  buf, err := json.MarshalIndent(t, "", "  ")
  if err != nil {
    fmt.Printf("failed to generate JSON: %s\n", err)
    return
  }

  fmt.Printf("%s\n", buf)
  fmt.Printf("aud -> '%s'\n", t.Audience())
  fmt.Printf("iat -> '%s'\n", t.IssuedAt().Format(time.RFC3339))
  if v, ok := t.Get(`privateClaimKey`); ok {
    fmt.Printf("privateClaimKey -> '%s'\n", v)
  }
  fmt.Printf("sub -> '%s'\n", t.Subject())
}
```