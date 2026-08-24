package mtproto

import (
	"testing"
	"time"

	"github.com/keenetic-mtproto/keenetic-mtproto/internal/config"
)

func TestWorkerCooldown_SkipsPlan(t *testing.T) {
	t.Cleanup(workerResetState)
	workerPenalize("dead.user.workers.dev", workerNotFoundCooldown)

	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		CFWorkerDomain: "dead.user.workers.dev",
		CFProxyEnabled: false,
	}
	plans, err := planTransports(cfg, config.QueueConfig{}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range plans {
		if p.isWorker {
			t.Fatalf("cooled worker must not appear in plans, got %+v", plans)
		}
	}
}

func TestWorkerCooldown_Expires(t *testing.T) {
	t.Cleanup(workerResetState)
	workerPenalize("ok.user.workers.dev", -time.Second)
	if workerInCooldown("ok.user.workers.dev") {
		t.Fatal("expired worker cooldown should not skip")
	}
}

func TestIsWSNotFound(t *testing.T) {
	if !isWSNotFound(&wsHandshakeError{statusCode: 404, statusLine: "404 Not Found"}) {
		t.Fatal("404 should be not-found")
	}
	if !isWSNotFound(&wsHandshakeError{statusCode: 410, statusLine: "410 Gone"}) {
		t.Fatal("410 should be not-found")
	}
	if isWSNotFound(&wsHandshakeError{statusCode: 503, statusLine: "503 Service Unavailable"}) {
		t.Fatal("503 is rate-limit, not not-found")
	}
}
