package idgen

import (
	"regexp"
	"testing"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNew_MatchesUUIDv4Format(t *testing.T) {
	id := New()
	if !uuidV4Pattern.MatchString(id) {
		t.Errorf("New() = %q, does not match the UUIDv4 pattern", id)
	}
}

func TestNew_ProducesUniqueValues(t *testing.T) {
	seen := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		id := New()
		if seen[id] {
			t.Fatalf("New() produced a duplicate on iteration %d: %q", i, id)
		}
		seen[id] = true
	}
}
