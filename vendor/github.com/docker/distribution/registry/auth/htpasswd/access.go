// Package htpasswd provides a simple authentication scheme that checks for the
// user credential hash in an htpasswd formatted file in a configuration-determined
// location.
//
// This authentication method MUST be used under TLS, as simple token-replay attack is possible.
package htpasswd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/auth"
)

type accessController struct {
	realm    string
	path     string
	modtime  time.Time
	mu       sync.Mutex
	htpasswd *htpasswd
}

var _ auth.AccessController = &accessController{}

func newAccessController(options map[string]interface{}) (auth.AccessController, error) {
	realm, present := options["realm"]
	if _, ok := realm.(string); !present || !ok {
		return nil, fmt.Errorf(`"realm" must be set for htpasswd access controller`)
	}

	pathOpt, present := options["path"]
	path, ok := pathOpt.(string)
	if !present || !ok {
		return nil, fmt.Errorf(`"path" must be set for htpasswd access controller`)
	}
	if err := createHtpasswdFile(path); err != nil {
		return nil, err
	}
	return &accessController{realm: realm.(string), path: path}, nil
}

func (ac *accessController) Authorized(ctx context.Context, accessRecords ...auth.Access) (context.Context, error) {
	req, err := dcontext.GetRequest(ctx)
	if err != nil {
		return nil, err
	}

	username, password, ok := req.BasicAuth()
	if !ok {
		return nil, &challenge{
			realm: ac.realm,
			err:   auth.ErrInvalidCredential,
		}
	}

	// Dynamically parsing the latest account list
	fstat, err := os.Stat(ac.path)
	if err != nil {
		return nil, err
	}

	lastModified := fstat.ModTime()
	ac.mu.Lock()
	if ac.htpasswd == nil || !ac.modtime.Equal(lastModified) {
		ac.modtime = lastModified

		f, err := os.Open(ac.path)
		if err != nil {
			ac.mu.Unlock()
			return nil, err
		}
		defer f.Close()

		h, err := newHTPasswd(f)
		if err != nil {
			ac.mu.Unlock()
			return nil, err
		}
		ac.htpasswd = h
	}
	localHTPasswd := ac.htpasswd
	ac.mu.Unlock()

	if err := localHTPasswd.authenticateUser(username, password); err != nil {
		dcontext.GetLogger(ctx).Errorf("error authenticating user %q: %v", username, err)
		return nil, &challenge{
			realm: ac.realm,
			err:   auth.ErrAuthenticationFailure,
		}
	}

	return auth.WithUser(ctx, auth.UserInfo{Name: username}), nil
}

// challenge implements the auth.Challenge interface.
type challenge struct {
	realm string
	err   error
}

var _ auth.Challenge = challenge{}

// SetHeaders sets the basic challenge header on the response.
func (ch challenge) SetHeaders(r *http.Request, w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=%q", ch.realm))
}

func (ch challenge) Error() string {
	return fmt.Sprintf("basic authentication challenge for realm %q: %s", ch.realm, ch.err)
}

// createHtpasswdFile creates and populates htpasswd file with a new user in case the file is missing
func createHtpasswdFile(path string) error {
	if f, err := os.Open(path); err == nil {
		f.Close()
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("failed to open htpasswd path %s", err)
	}
	defer f.Close()
	var secretBytes [32]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return err
	}
	pass := base64.RawURLEncoding.EncodeToString(secretBytes[:])
	encryptedPass, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(fmt.Sprintf("docker:%s", string(encryptedPass[:])))); err != nil {
		return err
	}
	dcontext.GetLoggerWithFields(context.Background(), map[interface{}]interface{}{
		"user":     "docker",
		"password": pass,
	}).Warnf("htpasswd is missing, provisioning with default user")
	return nil
}

func init() {
	auth.Register("htpasswd", auth.InitFunc(newAccessController))
}
