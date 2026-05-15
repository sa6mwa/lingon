package sessionorder

import "strings"

// Key returns the canonical alphanumeric ordering key for a session.
func Key(name, id string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(id)
}

// Less reports whether the left session should sort before the right session.
func Less(leftName, leftID, rightName, rightID string) bool {
	leftKey := Key(leftName, leftID)
	rightKey := Key(rightName, rightID)
	if leftKey != rightKey {
		return leftKey < rightKey
	}
	return strings.TrimSpace(leftID) < strings.TrimSpace(rightID)
}
