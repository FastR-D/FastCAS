package httpapi_test

import (
	"bytes"
	"sync"
)

// Child processes may still write diagnostics while a failed contract reads them.
type processLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *processLog) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *processLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
