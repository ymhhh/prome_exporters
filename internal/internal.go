package internal

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"sync"
)

const maxDrainBytes int64 = 1 << 20

var ErrBodyTooLarge = errors.New("response body exceeds limit")

func IOClose(closer io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(closer, maxDrainBytes))
	_ = closer.Close()
}

// ReadAllLimited reads r up to limit bytes. A non-positive limit reads without a cap.
func ReadAllLimited(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%w (%d bytes)", ErrBodyTooLarge, limit)
	}
	return b, nil
}

type gzipReadCloser struct {
	pipeReader *io.PipeReader
	wg         sync.WaitGroup
}

func (g *gzipReadCloser) Read(p []byte) (int, error) {
	return g.pipeReader.Read(p)
}

func (g *gzipReadCloser) Close() error {
	err := g.pipeReader.Close()
	g.wg.Wait()
	return err
}

// CompressWithGzip takes an io.Reader as input and pipes
// it through a gzip.Writer returning an io.ReadCloser containing
// the gzipped data.
func CompressWithGzip(data io.Reader) (io.ReadCloser, error) {
	pipeReader, pipeWriter := io.Pipe()
	gzipWriter := gzip.NewWriter(pipeWriter)

	rc := &gzipReadCloser{
		pipeReader: pipeReader,
	}

	rc.wg.Add(1)
	go func() {
		defer rc.wg.Done()
		_, err := io.Copy(gzipWriter, data)
		gzipWriter.Close()
		pipeWriter.CloseWithError(err)
	}()

	return rc, nil
}
