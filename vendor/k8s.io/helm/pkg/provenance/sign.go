/*
Copyright The Helm Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provenance

import (
	"bytes"
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/clearsign"
	"golang.org/x/crypto/openpgp/packet"

	"k8s.io/helm/pkg/chartutil"
	hapi "k8s.io/helm/pkg/proto/hapi/chart"
)

var defaultPGPConfig = packet.Config{
	DefaultHash: crypto.SHA512,
}

// SumCollection represents a collection of file and image checksums.
//
// Files are of the form:
//	FILENAME: "sha256:SUM"
// Images are of the form:
//	"IMAGE:TAG": "sha256:SUM"
// Docker optionally supports sha512, and if this is the case, the hash marker
// will be 'sha512' instead of 'sha256'.
type SumCollection struct {
	Files  map[string]string `json:"files"`
	Images map[string]string `json:"images,omitempty"`
}

// Verification contains information about a verification operation.
type Verification struct {
	// SignedBy contains the entity that signed a chart.
	SignedBy *openpgp.Entity
	// FileHash is the hash, prepended with the scheme, for the file that was verified.
	FileHash string
	// FileName is the name of the file that FileHash verifies.
	FileName string
}

// Signatory signs things.
//
// Signatories can be constructed from a PGP private key file using NewFromFiles
// or they can be constructed manually by setting the Entity to a valid
// PGP entity.
//
// The same Signatory can be used to sign or validate multiple charts.
type Signatory struct {
	// The signatory for this instance of Helm. This is used for signing.
	Entity *openpgp.Entity
	// The keyring for this instance of Helm. This is used for verification.
	KeyRing openpgp.EntityList
}

// NewFromFiles constructs a new Signatory from the PGP key in the given filename.
//
// This will emit an error if it cannot find a valid GPG keyfile (entity) at the
// given location.
//
// Note that the keyfile may have just a public key, just a private key, or
// both. The Signatory methods may have different requirements of the keys. For
// example, ClearSign must have a valid `openpgp.Entity.PrivateKey` before it
// can sign something.
func NewFromFiles(keyfile, keyringfile string) (*Signatory, error) {
	e, err := loadKey(keyfile)
	if err != nil {
		return nil, err
	}

	ring, err := loadKeyRing(keyringfile)
	if err != nil {
		return nil, err
	}

	return &Signatory{
		Entity:  e,
		KeyRing: ring,
	}, nil
}

// NewFromKeyring reads a keyring file and creates a Signatory.
//
// If id is not the empty string, this will also try to find an Entity in the
// keyring whose name matches, and set that as the signing entity. It will return
// an error if the id is not empty and also not found.
func NewFromKeyring(keyringfile, id string) (*Signatory, error) {
	ring, err := loadKeyRing(keyringfile)
	if err != nil {
		return nil, err
	}

	s := &Signatory{KeyRing: ring}

	// If the ID is empty, we can return now.
	if id == "" {
		return s, nil
	}

	// We're gonna go all GnuPG on this and look for a string that _contains_. If
	// two or more keys contain the string and none are a direct match, we error
	// out.
	var candidate *openpgp.Entity
	vague := false
	for _, e := range ring {
		for n := range e.Identities {
			if n == id {
				s.Entity = e
				return s, nil
			}
			if strings.Contains(n, id) {
				if candidate != nil {
					vague = true
				}
				candidate = e
			}
		}
	}
	if vague {
		return s, fmt.Errorf("more than one key contain the id %q", id)
	}

	s.Entity = candidate
	return s, nil
}

// PassphraseFetcher returns a passphrase for decrypting keys.
//
// This is used as a callback to read a passphrase from some other location. The
// given name is the Name field on the key, typically of the form:
//
//	USER_NAME (COMMENT) <EMAIL>
type PassphraseFetcher func(name string) ([]byte, error)

// DecryptKey decrypts a private key in the Signatory.
//
// If the key is not encrypted, this will return without error.
//
// If the key does not exist, this will return an error.
//
// If the key exists, but cannot be unlocked with the passphrase returned by
// the PassphraseFetcher, this will return an error.
//
// If the key is successfully unlocked, it will return nil.
func (s *Signatory) DecryptKey(fn PassphraseFetcher) error {
	if s.Entity == nil {
		return errors.New("private key not found")
	} else if s.Entity.PrivateKey == nil {
		return errors.New("provided key is not a private key")
	}

	// Nothing else to do if key is not encrypted.
	if !s.Entity.PrivateKey.Encrypted {
		return nil
	}

	fname := "Unknown"
	for i := range s.Entity.Identities {
		if i != "" {
			fname = i
			break
		}
	}

	p, err := fn(fname)
	if err != nil {
		return err
	}

	return s.Entity.PrivateKey.Decrypt(p)
}

// ClearSign signs a chart with the given key.
//
// This takes the path to a chart archive file and a key, and it returns a clear signature.
//
// The Signatory must have a valid Entity.PrivateKey for this to work. If it does
// not, an error will be returned.
func (s *Signatory) ClearSign(chartpath string) (string, error) {
	if s.Entity == nil {
		return "", errors.New("private key not found")
	} else if s.Entity.PrivateKey == nil {
		return "", errors.New("provided key is not a private key")
	}

	if fi, err := os.Stat(chartpath); err != nil {
		return "", err
	} else if fi.IsDir() {
		return "", errors.New("cannot sign a directory")
	}

	out := bytes.NewBuffer(nil)

	b, err := messageBlock(chartpath)
	if err != nil {
		return "", nil
	}

	// Sign the buffer
	w, err := clearsign.Encode(out, s.Entity.PrivateKey, &defaultPGPConfig)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(w, b)
	w.Close()
	return out.String(), err
}

// Verify checks a signature and verifies that it is legit for a chart.
func (s *Signatory) Verify(chartpath, sigpath string) (*Verification, error) {
	ver := &Verification{}
	for _, fname := range []string{chartpath, sigpath} {
		if fi, err := os.Stat(fname); err != nil {
			return ver, err
		} else if fi.IsDir() {
			return ver, fmt.Errorf("%s cannot be a directory", fname)
		}
	}

	// First verify the signature
	sig, err := s.decodeSignature(sigpath)
	if err != nil {
		return ver, fmt.Errorf("failed to decode signature: %s", err)
	}

	by, err := s.verifySignature(sig)
	if err != nil {
		return ver, err
	}
	ver.SignedBy = by

	// Second, verify the hash of the tarball.
	sum, err := DigestFile(chartpath)
	if err != nil {
		return ver, err
	}
	_, sums, err := parseMessageBlock(sig.Plaintext)
	if err != nil {
		return ver, err
	}

	sum = "sha256:" + sum
	basename := filepath.Base(chartpath)
	if sha, ok := sums.Files[basename]; !ok {
		return ver, fmt.Errorf("provenance does not contain a SHA for a file named %q", basename)
	} else if sha != sum {
		return ver, fmt.Errorf("sha256 sum does not match for %s: %q != %q", basename, sha, sum)
	}
	ver.FileHash = sum
	ver.FileName = basename

	// TODO: when image signing is added, verify that here.

	return ver, nil
}

func (s *Signatory) decodeSignature(filename string) (*clearsign.Block, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	block, _ := clearsign.Decode(data)
	if block == nil {
		// There was no sig in the file.
		return nil, errors.New("signature block not found")
	}

	return block, nil
}

// verifySignature verifies that the given block is validly signed, and returns the signer.
func (s *Signatory) verifySignature(block *clearsign.Block) (*openpgp.Entity, error) {
	return openpgp.CheckDetachedSignature(
		s.KeyRing,
		bytes.NewBuffer(block.Bytes),
		block.ArmoredSignature.Body,
	)
}

func messageBlock(chartpath string) (*bytes.Buffer, error) {
	var b *bytes.Buffer
	// Checksum the archive
	chash, err := DigestFile(chartpath)
	if err != nil {
		return b, err
	}

	base := filepath.Base(chartpath)
	sums := &SumCollection{
		Files: map[string]string{
			base: "sha256:" + chash,
		},
	}

	// Load the archive into memory.
	chart, err := chartutil.LoadFile(chartpath)
	if err != nil {
		return b, err
	}

	// Buffer a hash + checksums YAML file
	data, err := yaml.Marshal(chart.Metadata)
	if err != nil {
		return b, err
	}

	// FIXME: YAML uses ---\n as a file start indicator, but this is not legal in a PGP
	// clearsign block. So we use ...\n, which is the YAML document end marker.
	// http://yaml.org/spec/1.2/spec.html#id2800168
	b = bytes.NewBuffer(data)
	b.WriteString("\n...\n")

	data, err = yaml.Marshal(sums)
	if err != nil {
		return b, err
	}
	b.Write(data)

	return b, nil
}

// parseMessageBlock
func parseMessageBlock(data []byte) (*hapi.Metadata, *SumCollection, error) {
	// This sucks.
	parts := bytes.Split(data, []byte("\n...\n"))
	if len(parts) < 2 {
		return nil, nil, errors.New("message block must have at least two parts")
	}

	md := &hapi.Metadata{}
	sc := &SumCollection{}

	if err := yaml.Unmarshal(parts[0], md); err != nil {
		return md, sc, err
	}
	err := yaml.Unmarshal(parts[1], sc)
	return md, sc, err
}

// loadKey loads a GPG key found at a particular path.
func loadKey(keypath string) (*openpgp.Entity, error) {
	f, err := os.Open(keypath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	pr := packet.NewReader(f)
	return openpgp.ReadEntity(pr)
}

func loadKeyRing(ringpath string) (openpgp.EntityList, error) {
	f, err := os.Open(ringpath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return openpgp.ReadKeyRing(f)
}

// DigestFile calculates a SHA256 hash (like Docker) for a given file.
//
// It takes the path to the archive file, and returns a string representation of
// the SHA256 sum.
//
// The intended use of this function is to generate a sum of a chart TGZ file.
func DigestFile(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return Digest(f)
}

// Digest hashes a reader and returns a SHA256 digest.
//
// Helm uses SHA256 as its default hash for all non-cryptographic applications.
func Digest(in io.Reader) (string, error) {
	hash := crypto.SHA256.New()
	io.Copy(hash, in)
	return hex.EncodeToString(hash.Sum(nil)), nil
}
