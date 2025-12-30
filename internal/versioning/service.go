// Package versioning provides object versioning support for NebulaIO.
//
// When versioning is enabled on a bucket:
//   - Every object modification creates a new version
//   - Deleted objects become delete markers (recoverable)
//   - Previous versions can be retrieved by version ID
//   - Object Lock (WORM) can be applied to specific versions
//
// Version IDs are generated as unique, sortable identifiers that encode
// the creation timestamp, ensuring consistent ordering.
//
// Versioning states:
//   - Disabled: No versions kept (default)
//   - Enabled: All versions retained
//   - Suspended: New versions not created, existing preserved
//
// Use lifecycle rules to manage version retention and control storage costs.
package versioning

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
)

// Service handles versioning operations.
type Service struct {
	lastGen time.Time
	mu      sync.Mutex
	entropy uint16
}

// NewService creates a new versioning service.
func NewService() *Service {
	return &Service{}
}

// GenerateVersionID generates a ULID-based version ID for sortable versioning
// ULIDs are 26 characters, lexicographically sortable, and encode time
// Format: 10 chars timestamp + 16 chars randomness (Crockford Base32).
func (s *Service) GenerateVersionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	// Reset entropy if we're in a new millisecond
	if now.UnixMilli() != s.lastGen.UnixMilli() {
		s.lastGen = now
		// Generate new random entropy
		var b [2]byte
		rand.Read(b[:])
		s.entropy = binary.BigEndian.Uint16(b[:])
	} else {
		// Increment entropy for same-millisecond generation
		s.entropy++
	}

	return encodeULID(now, s.entropy)
}

// IsVersioningEnabled returns true if versioning is enabled for the bucket.
func IsVersioningEnabled(bucket *metadata.Bucket) bool {
	return bucket.Versioning == metadata.VersioningEnabled
}

// IsVersioningSuspended returns true if versioning is suspended for the bucket.
func IsVersioningSuspended(bucket *metadata.Bucket) bool {
	return bucket.Versioning == metadata.VersioningSuspended
}

// NullVersionID is used for objects created when versioning is suspended.
const NullVersionID = "null"

// Crockford's Base32 alphabet (excludes I, L, O, U to avoid confusion).
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// encodeULID encodes a timestamp and entropy into a ULID string.
func encodeULID(t time.Time, entropy uint16) string {
	// ULID is 26 characters: 10 for timestamp (48 bits) + 16 for randomness (80 bits)
	// We use 48 bits of millisecond timestamp + 80 bits of randomness
	//nolint:gosec // G115: UnixMilli is always positive for current time
	ms := uint64(t.UnixMilli())

	// Generate 80 bits of randomness
	var randomBytes [10]byte
	rand.Read(randomBytes[:])

	// Include our entropy counter in the first 2 bytes for uniqueness
	binary.BigEndian.PutUint16(randomBytes[:2], entropy)

	// Encode to Crockford Base32
	ulid := make([]byte, 26)

	// Encode timestamp (10 characters for 48 bits)
	ulid[0] = crockfordAlphabet[(ms>>45)&0x1F]
	ulid[1] = crockfordAlphabet[(ms>>40)&0x1F]
	ulid[2] = crockfordAlphabet[(ms>>35)&0x1F]
	ulid[3] = crockfordAlphabet[(ms>>30)&0x1F]
	ulid[4] = crockfordAlphabet[(ms>>25)&0x1F]
	ulid[5] = crockfordAlphabet[(ms>>20)&0x1F]
	ulid[6] = crockfordAlphabet[(ms>>15)&0x1F]
	ulid[7] = crockfordAlphabet[(ms>>10)&0x1F]
	ulid[8] = crockfordAlphabet[(ms>>5)&0x1F]
	ulid[9] = crockfordAlphabet[ms&0x1F]

	// Encode randomness (16 characters for 80 bits)
	ulid[10] = crockfordAlphabet[(randomBytes[0]>>3)&0x1F]
	ulid[11] = crockfordAlphabet[((randomBytes[0]<<2)|(randomBytes[1]>>6))&0x1F]
	ulid[12] = crockfordAlphabet[(randomBytes[1]>>1)&0x1F]
	ulid[13] = crockfordAlphabet[((randomBytes[1]<<4)|(randomBytes[2]>>4))&0x1F]
	ulid[14] = crockfordAlphabet[((randomBytes[2]<<1)|(randomBytes[3]>>7))&0x1F]
	ulid[15] = crockfordAlphabet[(randomBytes[3]>>2)&0x1F]
	ulid[16] = crockfordAlphabet[((randomBytes[3]<<3)|(randomBytes[4]>>5))&0x1F]
	ulid[17] = crockfordAlphabet[randomBytes[4]&0x1F]
	ulid[18] = crockfordAlphabet[(randomBytes[5]>>3)&0x1F]
	ulid[19] = crockfordAlphabet[((randomBytes[5]<<2)|(randomBytes[6]>>6))&0x1F]
	ulid[20] = crockfordAlphabet[(randomBytes[6]>>1)&0x1F]
	ulid[21] = crockfordAlphabet[((randomBytes[6]<<4)|(randomBytes[7]>>4))&0x1F]
	ulid[22] = crockfordAlphabet[((randomBytes[7]<<1)|(randomBytes[8]>>7))&0x1F]
	ulid[23] = crockfordAlphabet[(randomBytes[8]>>2)&0x1F]
	ulid[24] = crockfordAlphabet[((randomBytes[8]<<3)|(randomBytes[9]>>5))&0x1F]
	ulid[25] = crockfordAlphabet[randomBytes[9]&0x1F]

	return string(ulid)
}

// CompareVersionIDs compares two version IDs
// Returns -1 if a < b, 0 if a == b, 1 if a > b
// Newer versions have lexicographically greater IDs.
func CompareVersionIDs(a, b string) int {
	// Handle null version specially - it's always "oldest"
	if a == NullVersionID && b == NullVersionID {
		return 0
	}

	if a == NullVersionID {
		return -1
	}

	if b == NullVersionID {
		return 1
	}

	// ULID strings are lexicographically sortable
	if a < b {
		return -1
	}

	if a > b {
		return 1
	}

	return 0
}
