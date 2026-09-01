package cloud

import (
	"bytes"
	"fmt"
	"io"
)

func bytesReader(value string) *bytes.Reader { return bytes.NewReader([]byte(value)) }

func readAllLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("content exceeds %d bytes", limit)
	}
	return data, nil
}
