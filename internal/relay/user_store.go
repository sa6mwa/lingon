package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// UserStore persists users to disk.
type UserStore struct {
	mu sync.RWMutex

	Users map[string]User `json:"users"`
}

// NewUserStore returns an initialized user store.
func NewUserStore() *UserStore {
	return &UserStore{Users: make(map[string]User)}
}

// LoadUserStore reads users from the provided file path.
func LoadUserStore(path string) (*UserStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewUserStore(), nil
		}
		return nil, err
	}
	return LoadUserStoreFromBytes(data)
}

// LoadUserStoreFromBytes parses user store data.
func LoadUserStoreFromBytes(data []byte) (*UserStore, error) {
	var store UserStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	if store.Users == nil {
		store.Users = make(map[string]User)
	}
	for username, user := range store.Users {
		if user.Username == "" {
			user.Username = username
			store.Users[username] = user
		}
	}
	return &store, nil
}

// Save writes the user store to disk.
func (s *UserStore) Save(path string) error {
	if s == nil {
		return fmt.Errorf("user store is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ReplaceUsers replaces the users map.
func (s *UserStore) ReplaceUsers(users map[string]User) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if users == nil {
		users = make(map[string]User)
	}
	s.Users = make(map[string]User, len(users))
	for username, user := range users {
		if user.Username == "" {
			user.Username = username
		}
		s.Users[username] = user
	}
}

// ReloadFromDisk replaces users with the data in the file.
func (s *UserStore) ReloadFromDisk(path string) error {
	if s == nil {
		return fmt.Errorf("user store is nil")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	loaded, err := LoadUserStoreFromBytes(data)
	if err != nil {
		return err
	}
	s.ReplaceUsers(loaded.Users)
	return nil
}

// Get retrieves a user by username.
func (s *UserStore) Get(username string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.Users[username]
	return user, ok
}

// Upsert inserts or updates a user.
func (s *UserStore) Upsert(user User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Users == nil {
		s.Users = make(map[string]User)
	}
	s.Users[user.Username] = user
}

// Delete removes a user by username.
func (s *UserStore) Delete(username string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.Users[username]
	if ok {
		delete(s.Users, username)
	}
	return user, ok
}

// List returns users sorted by username.
func (s *UserStore) List() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]User, 0, len(s.Users))
	for _, user := range s.Users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].Username < users[j].Username
	})
	return users
}
