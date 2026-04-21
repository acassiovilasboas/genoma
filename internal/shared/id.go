package shared

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropyPool = sync.Pool{
		New: func() any {
			return ulid.Monotonic(rand.Reader, 0)
		},
	}
)

// NewID generates a new ULID string. ULIDs are sortable by time,
// making them ideal for distributed systems and database indexing.
func NewID() string {
	entropy := entropyPool.Get().(*ulid.MonotonicEntropy)
	defer entropyPool.Put(entropy)
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}
