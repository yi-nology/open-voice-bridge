package asr

import (
	"bytes"
	"io"
)

// limitedBuffer keeps only the first limit bytes while accepting everything,
// so a chatty child process cannot grow the agent's memory.
type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if b.limit <= 0 {
		b.limit = 4096
	}
	if b.buffer.Len() < b.limit {
		room := b.limit - b.buffer.Len()
		if room > len(data) {
			room = len(data)
		}
		if _, err := b.buffer.Write(data[:room]); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

var _ io.Writer = (*limitedBuffer)(nil)
