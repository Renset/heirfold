package server

import (
	"sync"
	"time"
)

// bucketRule says how fast a token bucket refills.
type bucketRule struct {
	capacity float64
	perSec   float64
}

// The limits of the sensitive endpoints.
var (
	// A client asks for few sign-in codes. The limit per client and address is
	// the smallest. The limit per address alone is larger, so one attacker
	// cannot lock the owner out.
	ruleCodeIP      = bucketRule{capacity: 10, perSec: 1.0 / 60}
	ruleCodeUser    = bucketRule{capacity: 3, perSec: 1.0 / 300}
	ruleCodeAccount = bucketRule{capacity: 10, perSec: 1.0 / 360}
	ruleVerifyIP    = bucketRule{capacity: 20, perSec: 1.0 / 15}
	// ruleCodeTrusted serves a browser that signed in to the account before.
	// A stranger cannot use it up, so the owner always gets a code.
	ruleCodeTrusted = bucketRule{capacity: 5, perSec: 1.0 / 120}
	// ruleAuditAnon limits the audit lines of clients without an account, so
	// a client with many addresses cannot push the real lines out of the log.
	ruleAuditAnon = bucketRule{capacity: 30, perSec: 1.0 / 60}
	// ruleRender limits new decodes of pictures for one account.
	ruleRender = bucketRule{capacity: 120, perSec: 2}
	// ruleUpload limits uploads of pictures for one account.
	ruleUpload = bucketRule{capacity: 30, perSec: 1.0 / 20}
	// ruleGalleryUpload lets an editor add an album at one time.
	ruleGalleryUpload = bucketRule{capacity: 200, perSec: 1.0 / 5}
	// One look at a map of streets takes some dozens of pictures.
	ruleMapTile = bucketRule{capacity: 900, perSec: 15}
	// A search for a place runs while an editor types a name.
	rulePlaceSearch = bucketRule{capacity: 120, perSec: 2}
	// ruleMailGlobal protects the allowance of the mail service.
	ruleMailGlobal  = bucketRule{capacity: 60, perSec: 1.0 / 60}
	ruleInvite      = bucketRule{capacity: 30, perSec: 1.0 / 120}
	ruleInviteAgain = bucketRule{capacity: 2, perSec: 1.0 / 600}
	// ruleEdit stops a script that floods the journal of edits.
	ruleEdit = bucketRule{capacity: 120, perSec: 2}
	// ruleExport limits how often one person builds a file of the whole tree.
	ruleExport        = bucketRule{capacity: 6, perSec: 1.0 / 300}
	rulePublic        = bucketRule{capacity: 10, perSec: 1.0 / 30}
	ruleAuditThrottle = bucketRule{capacity: 1, perSec: 1.0 / 300}
	ruleOverflow      = bucketRule{capacity: 50, perSec: 1}
)

type bucket struct {
	tokens float64
	last   time.Time
	rule   bucketRule
}

const (
	maxBuckets    = 100_000
	sweepInterval = 10 * time.Second
)

// rateLimits is a set of token buckets in memory.
type rateLimits struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	now       func() time.Time
	lastSweep time.Time
}

func newRateLimits() *rateLimits {
	return &rateLimits{buckets: make(map[string]*bucket), now: time.Now}
}

// overflowKey is the bucket that new clients share while the map is full.
const overflowKey = "overflow"

// allow takes one token. It returns false if the bucket is empty.
func (l *rateLimits) allow(key string, rule bucketRule) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	// A scan of the full map is slow, so a flood must not trigger one per key.
	if !ok && len(l.buckets) >= maxBuckets && now.Sub(l.lastSweep) >= sweepInterval {
		l.sweepLocked(now)
		l.lastSweep = now
	}
	if !ok && len(l.buckets) >= maxBuckets {
		// A flood of keys cannot grow the map without limit. New clients share
		// one bucket until the map has space, so nobody is locked out for good.
		key, rule = overflowKey, ruleOverflow
		b, ok = l.buckets[key]
	}
	if !ok {
		b = &bucket{tokens: rule.capacity, last: now, rule: rule}
		l.buckets[key] = b
	}
	b.tokens = min(rule.capacity, b.tokens+now.Sub(b.last).Seconds()*rule.perSec)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep removes the buckets that are full again.
func (l *rateLimits) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(l.now())
}

func (l *rateLimits) sweepLocked(now time.Time) {
	for key, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*b.rule.perSec >= b.rule.capacity {
			delete(l.buckets, key)
		}
	}
}
