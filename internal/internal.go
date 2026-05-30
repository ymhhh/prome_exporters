package internal

import (
	"compress/gzip"
	"io"
	"sync"
)

func IOClose(closer io.ReadCloser) {
	_, _ = io.Copy(io.Discard, closer)
	_ = closer.Close()
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
