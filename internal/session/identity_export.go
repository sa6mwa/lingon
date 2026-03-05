package session

// DefaultSessionIdentity returns the standard default session id/name pair.
func DefaultSessionIdentity() (string, string) {
	return defaultSessionIdentity()
}
