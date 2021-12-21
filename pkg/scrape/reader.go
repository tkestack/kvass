package scrape

import "io"

func wrapReader(reader io.ReadCloser, writer ...io.Writer) io.ReadCloser {
	return &wrappedReader{
		reader: reader,
		writer: writer,
	}
}

type wrappedReader struct {
	reader io.ReadCloser
	writer []io.Writer
}

func (w *wrappedReader) Read(p []byte) (n int, err error) {
	n, err = w.reader.Read(p)
	if err != nil {
		return n, err
	}

	for _, w := range w.writer {
		wTotal := 0
		for wTotal < n {
			wn, err := w.Write(p[wTotal:])
			if err != nil {
				return n, err
			}
			wTotal += wn
		}
	}

	return 0, nil
}

func (w *wrappedReader) Close() error {
	return w.reader.Close()
}
