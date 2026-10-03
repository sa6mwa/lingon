package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
)

// shareAuthFaults injects finite failures only in the isolated test relay.
type shareAuthFaults struct {
	remaining atomic.Int32
}

func (f *shareAuthFaults) wrap(base string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == base+"/__harness/share-auth-failures" {
			switch r.Method {
			case http.MethodPost:
				count, err := strconv.Atoi(r.URL.Query().Get("count"))
				if err != nil || count < 0 || count > 10 {
					http.Error(w, "count must be between 0 and 10", http.StatusBadRequest)
					return
				}
				f.remaining.Store(int32(count))
			case http.MethodGet:
			default:
				w.Header().Set("Allow", "GET, POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int32{"remaining": f.remaining.Load()})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == base+"/auth/share" {
			for count := f.remaining.Load(); count > 0; count = f.remaining.Load() {
				if f.remaining.CompareAndSwap(count, count-1) {
					w.Header().Set("Retry-After", "1")
					http.Error(w, "test relay restarting", http.StatusServiceUnavailable)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
