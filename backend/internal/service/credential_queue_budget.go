package service

import (
	"sync"
)

// Per-process payload protection complements the authoritative ticket count.
// It is not an execution slot and cannot approve upstream work. Existing tighter
// ingress limits still apply. A disconnected caller releases its memory budget.
type CredentialQueueBudget struct {
	mu       sync.Mutex
	bytes    int64
	requests int
	users    map[int64]int
}

func (b *CredentialQueueBudget) TryReserve(user int64, size int64) (func(), bool) {
	const maxBody int64 = 8 << 20
	const maxBytes int64 = 64 << 20
	const maxRequests = 100
	const maxPerUser = 10
	if size < 0 || size > maxBody {
		return nil, false
	}
	b.mu.Lock()
	if b.users == nil {
		b.users = map[int64]int{}
	}
	if b.bytes+size > maxBytes || b.requests >= maxRequests || b.users[user] >= maxPerUser {
		b.mu.Unlock()
		return nil, false
	}
	b.bytes += size
	b.requests++
	b.users[user]++
	b.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.bytes -= size
			b.requests--
			b.users[user]--
			if b.users[user] == 0 {
				delete(b.users, user)
			}
		})
	}, true
}
