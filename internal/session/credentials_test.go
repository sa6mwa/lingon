package session

import (
	"sync"
	"testing"
)

func TestRunnerRefreshKeepsStartupOptionsImmutable(t *testing.T) {
	r := New(Options{Token: "startup"})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 2000; i++ {
			r.setAccessToken("refreshed")
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 2000; i++ {
			token := r.accessToken()
			if token != "startup" && token != "refreshed" {
				t.Error("credential snapshot corrupted")
				return
			}
		}
	}()
	workers.Wait()
	if r.opts.Token != "startup" || r.accessToken() != "refreshed" {
		t.Fatal("refresh changed startup options or lost current credential")
	}
}
