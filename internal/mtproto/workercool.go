package mtproto

import (
	"errors"
	"sync"
	"time"

	"github.com/keenetic-mtproto/keenetic-mtproto/internal/log"
)

const (
	// workerNotFoundCooldown skips a Worker that returned 404/410 (deleted
	// script or Hello-World stub without /apiws). 30m is long enough that
	// the user can redeploy without every Telegram retry burning a handshake.
	workerNotFoundCooldown  = 30 * time.Minute
	workerRateLimitCooldown = 60 * time.Second
	// Skip a Worker that timed out so the next client can use the CF pool
	// instead of burning another 8s handshake. Short: a blip should recover.
	workerDialTimeoutCooldown = 30 * time.Second
)

var (
	workerCoolMu sync.Mutex
	workerCoolTo = map[string]time.Time{}
)

func workerInCooldown(domain string) bool {
	if domain == "" {
		return false
	}
	workerCoolMu.Lock()
	defer workerCoolMu.Unlock()
	t, ok := workerCoolTo[domain]
	if !ok {
		return false
	}
	if time.Now().After(t) {
		delete(workerCoolTo, domain)
		return false
	}
	return true
}

func workerPenalize(domain string, d time.Duration) {
	if domain == "" {
		return
	}
	workerCoolMu.Lock()
	defer workerCoolMu.Unlock()
	workerCoolTo[domain] = time.Now().Add(d)
}

func workerRecordSuccess(domain string) {
	if domain == "" {
		return
	}
	workerCoolMu.Lock()
	defer workerCoolMu.Unlock()
	delete(workerCoolTo, domain)
}

func workerResetState() {
	workerCoolMu.Lock()
	defer workerCoolMu.Unlock()
	workerCoolTo = map[string]time.Time{}
}

func isWSNotFound(err error) bool {
	var he *wsHandshakeError
	if !errors.As(err, &he) {
		return false
	}
	return he.statusCode == 404 || he.statusCode == 410
}

func noteWorkerNotFound(domain string) {
	log.Warnf("%s CF worker %s is 404/410 (missing /apiws). Skipping for %v — redeploy the tunnel Worker",
		tg(""), domain, workerNotFoundCooldown)
}
