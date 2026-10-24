package relay

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrStateOwned indicates another process owns the relay state directory.
var ErrStateOwned = errors.New("relay state is owned by another process")

// ErrStateOwnerClosed prevents a stopped owner from writing after releasing its lease.
var ErrStateOwnerClosed = errors.New("relay state owner is closed")

// StateOwner holds exclusive process ownership of a relay's persisted state.
type StateOwner struct {
	dir       string
	file      *os.File
	mu        sync.RWMutex
	closed    bool
	server    *http.Server
	done      chan error
	requests  sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

// NewStateOwner acquires ownership before callers load mutable relay state.
func NewStateOwner(dir string) (*StateOwner, error) {
	if dir == "" {
		return nil, fmt.Errorf("relay state directory is required")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("relay state directory must not be writable by other users: %s", dir)
	}
	file, err := acquireStateFile(filepath.Join(dir, "relay-state.lock"))
	if err != nil {
		return nil, err
	}
	return &StateOwner{dir: dir, file: file}, nil
}

func (o *StateOwner) withWrite(fn func() error) error {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.closed {
		return ErrStateOwnerClosed
	}
	return fn()
}

// LoadStore binds the loaded state to this owner's lifetime.
func (o *StateOwner) LoadStore() (*Store, error) {
	var s *Store
	err := o.withWrite(func() error {
		var err error
		s, err = LoadStore(o.dir)
		if err == nil {
			s.owner = o
		}
		return err
	})
	return s, err
}

// LoadUsers binds user-file writes to this owner's lifetime.
func (o *StateOwner) LoadUsers(path string) (*UserStore, error) {
	var s *UserStore
	err := o.withWrite(func() error {
		var err error
		s, err = LoadUserStore(path)
		if err == nil {
			s.owner = o
		}
		return err
	})
	return s, err
}

func userAdminSocket(dir string) string { return filepath.Join(dir, ".admin", "socket") }

type userAdminRequest struct {
	UsersFile userFileReference   `json:"users_file"`
	Mutation  UserMutationRequest `json:"mutation"`
}

type userAdminResponse struct {
	Result  UserMutationResult `json:"result"`
	Code    string             `json:"code,omitempty"`
	Message string             `json:"message,omitempty"`
}

// ListenUsers starts a private administrative socket backed by the live mutator.
func (o *StateOwner) ListenUsers(usersFile string, mutator UserMutator) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrStateOwnerClosed
	}
	if mutator == nil {
		return fmt.Errorf("local user mutator is required")
	}
	if o.server != nil {
		return fmt.Errorf("local administration is already listening")
	}
	path := userAdminSocket(o.dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("local administration directory must be private")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("local administration path is not a socket")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	listener, err := newUserAdminUnixListener(path)
	if err != nil {
		return fmt.Errorf("listen for local user administration: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return err
	}
	usersFile, err = filepath.Abs(usersFile)
	if err != nil {
		_ = listener.Close()
		return err
	}
	o.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	o.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.RLock()
		if o.closed {
			o.mu.RUnlock()
			writeError(w, http.StatusServiceUnavailable, ErrStateOwnerClosed.Error())
			return
		}
		o.requests.Add(1)
		o.mu.RUnlock()
		defer o.requests.Done()
		if r.Method != http.MethodPost || r.URL.Path != "/mutate" {
			writeError(w, http.StatusNotFound, "unsupported local administration request")
			return
		}
		var req userAdminRequest
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid local administration request")
			return
		}
		if !req.UsersFile.matches(usersFile) {
			writeError(w, http.StatusConflict, "users file does not match the running relay")
			return
		}
		result, err := mutator.MutateUser(r.Context(), req.Mutation)
		resp := userAdminResponse{Result: result}
		if err != nil {
			resp.Code, resp.Message = userAdminErrorCode(err), err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	})
	o.done = make(chan error, 1)
	go func() { o.done <- o.server.Serve(listener) }()
	return nil
}

// Close fences late writes, closes administration, and releases process ownership.
func (o *StateOwner) Close() error {
	o.closeOnce.Do(func() {
		o.mu.Lock()
		o.closed = true
		server, done := o.server, o.done
		o.mu.Unlock()
		var serverErr error
		if server != nil {
			serverErr = server.Close()
			<-done
			o.requests.Wait()
		}
		o.closeErr = errors.Join(serverErr, o.file.Close())
	})
	return o.closeErr
}

func userAdminErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrUserExists):
		return "user_exists"
	case errors.Is(err, ErrUserNotFound):
		return "user_not_found"
	case errors.Is(err, ErrUsernameRequired):
		return "username_required"
	case errors.Is(err, ErrUsernameInvalid):
		return "username_invalid"
	default:
		return "failed"
	}
}

func decodeUserAdminError(resp userAdminResponse) error {
	switch resp.Code {
	case "user_exists":
		return ErrUserExists
	case "user_not_found":
		return ErrUserNotFound
	case "username_required":
		return ErrUsernameRequired
	case "username_invalid":
		return ErrUsernameInvalid
	default:
		return fmt.Errorf("%s", resp.Message)
	}
}
