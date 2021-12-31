package scrape

import (
	"io"
)

func wrapReader(reader io.ReadCloser, writer ...io.Writer) *wrappedReader {
	return &wrappedReader{
		reader: reader,
		writer: writer,
	}
}

// wrappedReader copy data to writer when Read is called
type wrappedReader struct {
	reader io.ReadCloser
	writer []io.Writer
}

// Read implement io.Reader
func (w *wrappedReader) Read(p []byte) (n int, err error) {
	n, rerr := w.reader.Read(p)
	for _, w := range w.writer {
		wTotal := 0
		for wTotal < n {
			wn, werr := w.Write(p[wTotal:n])
			if werr != nil {
				return n, werr
			}
			wTotal += wn
		}
	}
	return n, rerr
}

// Close implement io.Closer
func (w *wrappedReader) Close() error {
	return w.reader.Close()
}
