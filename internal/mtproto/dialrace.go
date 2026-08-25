package mtproto

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/keenetic-mtproto/keenetic-mtproto/internal/log"
)

// transportRaceWidth is how many upstreams we try at once on Keenetic.
// Wide enough to find a live CF path in one RTT; narrow enough for MIPS.
const transportRaceWidth = 4

// leadingWorkerCount is how many dedicated Worker plans sit at the front of
// the list. Those are dialed as their own batch so a flaky public CF domain
// or a blackholed native WS edge cannot "win" the race over the user's Worker.
func leadingWorkerCount(plans []transportPlan) int {
	n := 0
	for n < len(plans) && plans[n].isWorker {
		n++
	}
	return n
}

func recordPlanFailure(p transportPlan, err error) {
	if err == nil {
		return
	}
	if isDialTimeout(err) {
		if key := p.cooldownKey(); key != "" {
			tcpRecordFailure(key)
		}
	}
	if p.kind != transportWS {
		return
	}
	if p.isWorker {
		switch {
		case isWSNotFound(err):
			workerPenalize(p.sni, workerNotFoundCooldown)
			noteWorkerNotFound(p.sni)
		case wsRateLimited(err):
			workerPenalize(p.sni, workerRateLimitCooldown)
		case isDialTimeout(err):
			workerPenalize(p.sni, workerDialTimeoutCooldown)
		}
		return
	}
	if p.cfBase != "" && wsRateLimited(err) {
		cfBalancerInst.penalize(p.cfBase, cfProxyDomainCooldown)
	}
}

func recordPlanSuccess(p transportPlan, dc int) {
	if p.kind == transportWS {
		wsRecordSuccess(dc)
		if p.isWorker {
			workerRecordSuccess(p.sni)
		}
		if p.cfBase != "" {
			if cfBalancerInst.pin(dc, p.cfBase) {
				log.Infof("%s DC %d switched active CF domain to %s", tg(""), dc, p.cfBase)
			}
		}
		return
	}
	tcpRecordSuccess(p.addr)
}

func filterReadyPlans(plans []transportPlan, dc int, tag string) []transportPlan {
	ready := make([]transportPlan, 0, len(plans))
	for _, p := range plans {
		if key := p.cooldownKey(); key != "" && tcpAddrInCooldown(key) {
			log.Debugf("%s DC %d skip %s (%s in cooldown)", tag, dc, p.describe(), key)
			continue
		}
		if p.isWorker && workerInCooldown(p.sni) {
			log.Debugf("%s DC %d skip %s (worker cooldown)", tag, dc, p.describe())
			continue
		}
		ready = append(ready, p)
	}
	return ready
}

func dialPlans(plans []transportPlan, mark uint, dc int, protoTag uint32, wsTimeout time.Duration, tag string) (*ObfuscatedConn, string, error) {
	ready := filterReadyPlans(plans, dc, tag)
	if len(ready) == 0 {
		return nil, "", fmt.Errorf("no transport available (all in cooldown or blacklisted)")
	}

	var (
		attempts    []string
		wsTried     int
		wsRedirects int
		cfFailed    int
	)
	addAttempt := func(p transportPlan, err error) {
		attempts = append(attempts, fmt.Sprintf("%s: %s", p.describe(), shortErr(err)))
		if p.kind != transportWS {
			return
		}
		wsTried++
		if isWSRedirect(err) {
			wsRedirects++
		}
		if p.cfBase != "" {
			cfFailed++
		}
	}

	i := 0
	if n := leadingWorkerCount(ready); n > 0 {
		obf, desc, fails := raceDialBatch(ready[:n], mark, dc, protoTag, wsTimeout, tag)
		for _, f := range fails {
			addAttempt(f.p, f.err)
		}
		if obf != nil {
			return obf, desc, nil
		}
		i = n
	}
	for ; i < len(ready); i += transportRaceWidth {
		end := i + transportRaceWidth
		if end > len(ready) {
			end = len(ready)
		}
		obf, desc, fails := raceDialBatch(ready[i:end], mark, dc, protoTag, wsTimeout, tag)
		for _, f := range fails {
			addAttempt(f.p, f.err)
		}
		if obf != nil {
			return obf, desc, nil
		}
	}

	if wsTried > 0 {
		wsRecordFailure(dc, wsRedirects == wsTried)
	}
	if cfFailed > 0 {
		requestCFPoolRefresh()
	}
	if len(attempts) == 0 {
		return nil, "", fmt.Errorf("no transport available (all in cooldown or blacklisted)")
	}
	return nil, "", fmt.Errorf("all transports failed: %s", strings.Join(attempts, "; "))
}

type racedFail struct {
	p   transportPlan
	err error
}

func raceDialBatch(batch []transportPlan, mark uint, dc int, protoTag uint32, wsTimeout time.Duration, tag string) (*ObfuscatedConn, string, []racedFail) {
	if len(batch) == 0 {
		return nil, "", nil
	}
	if len(batch) == 1 {
		return dialSinglePlan(batch[0], mark, dc, protoTag, wsTimeout, tag)
	}

	type won struct {
		obf  *ObfuscatedConn
		desc string
	}
	wonCh := make(chan won, 1)
	var (
		taken  atomic.Bool
		failMu sync.Mutex
		fails  []racedFail
		wg     sync.WaitGroup
	)
	for _, p := range batch {
		wg.Add(1)
		go func(p transportPlan) {
			defer wg.Done()
			log.Debugf("%s DC %d racing %s", tag, dc, p.describe())
			start := time.Now()
			obf, err := dialAndObfuscate(p, mark, dc, protoTag, wsTimeout)
			if err != nil {
				if taken.Load() {
					return
				}
				recordPlanFailure(p, err)
				log.Debugf("%s DC %d %s failed after %dms: %v", tag, dc, p.describe(), time.Since(start).Milliseconds(), err)
				failMu.Lock()
				fails = append(fails, racedFail{p: p, err: err})
				failMu.Unlock()
				return
			}
			if !taken.CompareAndSwap(false, true) {
				_ = obf.Close()
				return
			}
			recordPlanSuccess(p, dc)
			log.Infof("%s DC %d connected via %s in %dms", tag, dc, p.describe(), time.Since(start).Milliseconds())
			select {
			case wonCh <- won{obf: obf, desc: p.describe()}:
			default:
				_ = obf.Close()
			}
		}(p)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case w := <-wonCh:
		return w.obf, w.desc, nil
	case <-done:
		return nil, "", fails
	}
}

func dialSinglePlan(p transportPlan, mark uint, dc int, protoTag uint32, wsTimeout time.Duration, tag string) (*ObfuscatedConn, string, []racedFail) {
	log.Debugf("%s DC %d dialing %s", tag, dc, p.describe())
	start := time.Now()
	obf, err := dialAndObfuscate(p, mark, dc, protoTag, wsTimeout)
	if err != nil {
		recordPlanFailure(p, err)
		log.Debugf("%s DC %d %s failed after %dms: %v", tag, dc, p.describe(), time.Since(start).Milliseconds(), err)
		return nil, "", []racedFail{{p: p, err: err}}
	}
	recordPlanSuccess(p, dc)
	log.Infof("%s DC %d connected via %s in %dms", tag, dc, p.describe(), time.Since(start).Milliseconds())
	return obf, p.describe(), nil
}

func dialAndObfuscate(p transportPlan, mark uint, dc int, protoTag uint32, wsTimeout time.Duration) (*ObfuscatedConn, error) {
	var (
		conn net.Conn
		err  error
	)
	if p.kind == transportWS {
		conn, err = dialOneWS(p, mark, wsTimeout)
	} else {
		conn, err = dialOne(p, mark)
	}
	if err != nil {
		return nil, err
	}
	obf, oerr := completeObfuscation(conn, dc, protoTag)
	if oerr != nil {
		_ = conn.Close()
		return nil, oerr
	}
	return obf, nil
}
