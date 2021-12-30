package scrape

import (
	"bytes"
	"io"
	"testing"
)

func TestWrapReader(t *testing.T) {
	data := []byte("hello")
	p := []byte("he")
	buf := bytes.NewBuffer([]byte{})
	r := wrapReader(io.NopCloser(bytes.NewReader(data)), buf)
	for {
		_, err := r.Read(p)
		if err == io.EOF {
			break
		}
	}
	if string(data) != buf.String() {
		t.Fatalf("[%s]\n", buf.String())
	}
}
