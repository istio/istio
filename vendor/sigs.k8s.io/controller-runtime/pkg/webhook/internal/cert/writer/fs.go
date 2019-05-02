/*
Copyright 2018 The Kubernetes Authors.

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

package writer

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/generator"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/writer/atomic"
)

// fsCertWriter provisions the certificate by reading and writing to the filesystem.
type fsCertWriter struct {
	// dnsName is the DNS name that the certificate is for.
	dnsName string

	*FSCertWriterOptions
}

// FSCertWriterOptions are options for constructing a FSCertWriter.
type FSCertWriterOptions struct {
	// certGenerator generates the certificates.
	CertGenerator generator.CertGenerator
	// path is the directory that the certificate and private key and CA certificate will be written.
	Path string
}

var _ CertWriter = &fsCertWriter{}

func (ops *FSCertWriterOptions) setDefaults() {
	if ops.CertGenerator == nil {
		ops.CertGenerator = &generator.SelfSignedCertGenerator{}
	}
}

func (ops *FSCertWriterOptions) validate() error {
	if len(ops.Path) == 0 {
		return errors.New("path must be set in FSCertWriterOptions")
	}
	return nil
}

// NewFSCertWriter constructs a CertWriter that persists the certificate on filesystem.
func NewFSCertWriter(ops FSCertWriterOptions) (CertWriter, error) {
	ops.setDefaults()
	err := ops.validate()
	if err != nil {
		return nil, err
	}
	return &fsCertWriter{
		FSCertWriterOptions: &ops,
	}, nil
}

// EnsureCert provisions certificates for a webhookClientConfig by writing the certificates in the filesystem.
func (f *fsCertWriter) EnsureCert(dnsName string) (*generator.Artifacts, bool, error) {
	// create or refresh cert and write it to fs
	f.dnsName = dnsName
	return handleCommon(f.dnsName, f)
}

func (f *fsCertWriter) write() (*generator.Artifacts, error) {
	return f.doWrite()
}

func (f *fsCertWriter) overwrite() (*generator.Artifacts, error) {
	return f.doWrite()
}

func (f *fsCertWriter) doWrite() (*generator.Artifacts, error) {
	certs, err := f.CertGenerator.Generate(f.dnsName)
	if err != nil {
		return nil, err
	}

	// AtomicWriter's algorithm only manages files using symbolic link.
	// If a file is not a symbolic link, will ignore the update for it.
	// We want to cleanup for AtomicWriter by removing old files that are not symbolic links.
	err = prepareToWrite(f.Path)
	if err != nil {
		return nil, err
	}

	aw, err := atomic.NewAtomicWriter(f.Path, log.WithName("atomic-writer").
		WithValues("task", "processing webhook"))
	if err != nil {
		return nil, err
	}
	err = aw.Write(certToProjectionMap(certs))
	return certs, err
}

// prepareToWrite ensures it directory is compatible with the atomic.Writer library.
func prepareToWrite(dir string) error {
	_, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		log.Info("cert directory doesn't exist, creating", "directory", dir)
		// TODO: figure out if we can reduce the permission. (Now it's 0777)
		err = os.MkdirAll(dir, 0777)
		if err != nil {
			return fmt.Errorf("can't create dir: %v", dir)
		}
	case err != nil:
		return err
	}

	filenames := []string{CAKeyName, CACertName, ServerCertName, ServerKeyName}
	for _, f := range filenames {
		abspath := path.Join(dir, f)
		_, err := os.Stat(abspath)
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			log.Error(err, "unable to stat file", "file", abspath)
		}
		_, err = os.Readlink(abspath)
		// if it's not a symbolic link
		if err != nil {
			err = os.Remove(abspath)
			if err != nil {
				log.Error(err, "unable to remove old file", "file", abspath)
			}
		}
	}
	return nil
}

func (f *fsCertWriter) read() (*generator.Artifacts, error) {
	if err := ensureExist(f.Path); err != nil {
		return nil, err
	}
	caKeyBytes, err := ioutil.ReadFile(path.Join(f.Path, CAKeyName))
	if err != nil {
		return nil, err
	}
	caCertBytes, err := ioutil.ReadFile(path.Join(f.Path, CACertName))
	if err != nil {
		return nil, err
	}
	certBytes, err := ioutil.ReadFile(path.Join(f.Path, ServerCertName))
	if err != nil {
		return nil, err
	}
	keyBytes, err := ioutil.ReadFile(path.Join(f.Path, ServerKeyName))
	if err != nil {
		return nil, err
	}
	return &generator.Artifacts{
		CAKey:  caKeyBytes,
		CACert: caCertBytes,
		Cert:   certBytes,
		Key:    keyBytes,
	}, nil
}

func ensureExist(dir string) error {
	filenames := []string{CAKeyName, CACertName, ServerCertName, ServerKeyName}
	for _, filename := range filenames {
		_, err := os.Stat(path.Join(dir, filename))
		switch {
		case err == nil:
			continue
		case os.IsNotExist(err):
			return notFoundError{err}
		default:
			return err
		}
	}
	return nil
}

func certToProjectionMap(cert *generator.Artifacts) map[string]atomic.FileProjection {
	// TODO: figure out if we can reduce the permission. (Now it's 0666)
	return map[string]atomic.FileProjection{
		CAKeyName: {
			Data: cert.CAKey,
			Mode: 0666,
		},
		CACertName: {
			Data: cert.CACert,
			Mode: 0666,
		},
		ServerCertName: {
			Data: cert.Cert,
			Mode: 0666,
		},
		ServerKeyName: {
			Data: cert.Key,
			Mode: 0666,
		},
	}
}

func (f *fsCertWriter) Inject(objs ...runtime.Object) error {
	return nil
}
