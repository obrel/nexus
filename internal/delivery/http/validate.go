package http

import "regexp"

// validIDRegex matches alphanumeric characters, underscores, and hyphens (1-128 chars).
// This prevents NATS subject injection via wildcards (., >, *) and other special characters.
var validIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// isValidID checks if an ID matches the allowed format.
func isValidID(id string) bool {
	return validIDRegex.MatchString(id)
}
