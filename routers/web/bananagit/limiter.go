// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"sync"
	"time"
)

// findLimiter allows about 10 find requests per minute per account. It lives in memory: finds are cosmetic,
// so a restart forgetting the counts does no harm.
var findLimiter = newRateLimiter(10, time.Minute)

type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[int64][]time.Time
	swept  time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, now: time.Now, hits: map[int64][]time.Time{}}
}

// Allow counts one request of the account and reports whether it is within the limit.
func (l *rateLimiter) Allow(id int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	if now.Sub(l.swept) > l.window { // forget idle accounts now and then
		for key, times := range l.hits {
			if len(times) == 0 || !times[len(times)-1].After(cutoff) {
				delete(l.hits, key)
			}
		}
		l.swept = now
	}
	times := l.hits[id]
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[id] = kept
		return false
	}
	l.hits[id] = append(kept, now)
	return true
}
