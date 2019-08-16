package archiver

import (
	"compress/gzip"
	"fmt"
	"io"
	"path/filepath"
)

// Gz facilitates gzip compression.
type Gz struct {
	CompressionLevel int
}

// Compress reads in, compresses it, and writes it to out.
func (gz *Gz) Compress(in io.Reader, out io.Writer) error {
	w, err := gzip.NewWriterLevel(out, gz.CompressionLevel)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, in)
	return err
}

// Decompress reads in, decompresses it, and writes it to out.
func (gz *Gz) Decompress(in io.Reader, out io.Writer) error {
	r, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(out, r)
	return err
}

// CheckExt ensures the file extension matches the format.
func (gz *Gz) CheckExt(filename string) error {
	if filepath.Ext(filename) != ".gz" {
		return fmt.Errorf("filename must have a .gz extension")
	}
	return nil
}

func (gz *Gz) String() string { return "gz" }

// NewGz returns a new, default instance ready to be customized and used.
func NewGz() *Gz {
	return &Gz{
		CompressionLevel: gzip.DefaultCompression,
	}
}

// Compile-time checks to ensure type implements desired interfaces.
var (
	_ = Compressor(new(Gz))
	_ = Decompressor(new(Gz))
)

// DefaultGz is a default instance that is conveniently ready to use.
var DefaultGz = NewGz()
