package session

import (
	"bytes"
	"encoding/json"
	"io"
)

type jsonReader struct{ b []byte }

func newJSONReader(v any) *jsonReader {
	b, _ := json.Marshal(v)
	return &jsonReader{b: b}
}

func (j *jsonReader) reader() io.Reader {
	if j == nil {
		return nil
	}
	return bytes.NewReader(j.b)
}
