package terminal

import (
	"os"
	"sync"
	"testing"
)

func TestWithFileDescriptorFencesConcurrentClose(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { finished <- WithFileDescriptor(reader, func(uintptr) { close(entered); <-release }) }()
	<-entered
	var closing sync.WaitGroup
	closing.Add(1)
	go func() { defer closing.Done(); _ = reader.Close() }()
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	closing.Wait()
	called := false
	if err := WithFileDescriptor(reader, func(uintptr) { called = true }); err == nil || called {
		t.Fatal("closed descriptor reached callback")
	}
	if err := WithFileDescriptor(nil, func(uintptr) { t.Fatal("nil descriptor reached callback") }); err == nil {
		t.Fatal("nil file accepted")
	}
}
