// +build !noresumabledigest

package storage

import (
	"context"
	"encoding"
	"fmt"
	"hash"
	"path"
	"strconv"

	storagedriver "github.com/docker/distribution/registry/storage/driver"
	"github.com/sirupsen/logrus"
)

// resumeDigest attempts to restore the state of the internal hash function
// by loading the most recent saved hash state equal to the current size of the blob.
func (bw *blobWriter) resumeDigest(ctx context.Context) error {
	if !bw.resumableDigestEnabled {
		return errResumableDigestNotAvailable
	}

	h, ok := bw.digester.Hash().(encoding.BinaryUnmarshaler)
	if !ok {
		return errResumableDigestNotAvailable
	}

	offset := bw.fileWriter.Size()
	if offset == bw.written {
		// State of digester is already at the requested offset.
		return nil
	}

	// List hash states from storage backend.
	var hashStateMatch hashStateEntry
	hashStates, err := bw.getStoredHashStates(ctx)
	if err != nil {
		return fmt.Errorf("unable to get stored hash states with offset %d: %s", offset, err)
	}

	// Find the highest stored hashState with offset equal to
	// the requested offset.
	for _, hashState := range hashStates {
		if hashState.offset == offset {
			hashStateMatch = hashState
			break // Found an exact offset match.
		}
	}

	if hashStateMatch.offset == 0 {
		// No need to load any state, just reset the hasher.
		h.(hash.Hash).Reset()
	} else {
		storedState, err := bw.driver.GetContent(ctx, hashStateMatch.path)
		if err != nil {
			return err
		}

		if err = h.UnmarshalBinary(storedState); err != nil {
			return err
		}
		bw.written = hashStateMatch.offset
	}

	// Mind the gap.
	if gapLen := offset - bw.written; gapLen > 0 {
		return errResumableDigestNotAvailable
	}

	return nil
}

type hashStateEntry struct {
	offset int64
	path   string
}

// getStoredHashStates returns a slice of hashStateEntries for this upload.
func (bw *blobWriter) getStoredHashStates(ctx context.Context) ([]hashStateEntry, error) {
	uploadHashStatePathPrefix, err := pathFor(uploadHashStatePathSpec{
		name: bw.blobStore.repository.Named().String(),
		id:   bw.id,
		alg:  bw.digester.Digest().Algorithm(),
		list: true,
	})

	if err != nil {
		return nil, err
	}

	paths, err := bw.blobStore.driver.List(ctx, uploadHashStatePathPrefix)
	if err != nil {
		if _, ok := err.(storagedriver.PathNotFoundError); !ok {
			return nil, err
		}
		// Treat PathNotFoundError as no entries.
		paths = nil
	}

	hashStateEntries := make([]hashStateEntry, 0, len(paths))

	for _, p := range paths {
		pathSuffix := path.Base(p)
		// The suffix should be the offset.
		offset, err := strconv.ParseInt(pathSuffix, 0, 64)
		if err != nil {
			logrus.Errorf("unable to parse offset from upload state path %q: %s", p, err)
		}

		hashStateEntries = append(hashStateEntries, hashStateEntry{offset: offset, path: p})
	}

	return hashStateEntries, nil
}

func (bw *blobWriter) storeHashState(ctx context.Context) error {
	if !bw.resumableDigestEnabled {
		return errResumableDigestNotAvailable
	}

	h, ok := bw.digester.Hash().(encoding.BinaryMarshaler)
	if !ok {
		return errResumableDigestNotAvailable
	}

	state, err := h.MarshalBinary()
	if err != nil {
		return err
	}

	uploadHashStatePath, err := pathFor(uploadHashStatePathSpec{
		name:   bw.blobStore.repository.Named().String(),
		id:     bw.id,
		alg:    bw.digester.Digest().Algorithm(),
		offset: bw.written,
	})

	if err != nil {
		return err
	}

	return bw.driver.PutContent(ctx, uploadHashStatePath, state)
}
