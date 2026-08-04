// Package idgen generates the IDs used for every resource in Denizen
// (users, items, shares, ...). Deliberately hand-rolled instead of pulling
// in a UUID library: generating a UUIDv4 is a dozen lines around
// crypto/rand, well within the "implement it ourselves" bar from
// CONTRIBUTING.md.
package idgen

import (
	"crypto/rand"
	"fmt"
)

// New returns a random UUID (version 4, RFC 4122 variant), formatted as the
// usual 36-character hyphenated string.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read only fails if the OS RNG is unavailable, which
		// means nothing on this system can be trusted to generate secrets.
		panic("idgen: crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
