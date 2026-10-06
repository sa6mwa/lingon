package testutil

import (
	"strings"
	"sync"
	"testing"
)

func TestOutputBufferConcurrentSnapshotsRetainEveryWrite(t *testing.T) {
	var buffer OutputBuffer
	var writers sync.WaitGroup
	writers.Add(2)
	done := make(chan struct{})
	for _, marker := range []string{"first\n", "second\n"} {
		go func() {
			defer writers.Done()
			for i := 0; i < 500; i++ {
				_, _ = buffer.Write([]byte(marker))
			}
		}()
	}
	go func() { writers.Wait(); close(done) }()
	for {
		_ = buffer.String()
		select {
		case <-done:
			text := buffer.String()
			if strings.Count(text, "first\n") != 500 || strings.Count(text, "second\n") != 500 {
				t.Fatal("snapshot lost or interleaved a complete write")
			}
			return
		default:
		}
	}
}
