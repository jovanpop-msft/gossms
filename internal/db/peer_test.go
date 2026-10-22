package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gosmo "github.com/radix29/gosmo"
	"github.com/radix29/gossms/internal/config"
)

// newTestConn builds a ServerConn over a fake pool with no server behind it,
// so Close cancels Server.Context() as a real connection's does.
func newTestConn(server string, registeredPeers ...string) *ServerConn {
	pool, err := sql.Open("captestdb", "")
	if err != nil {
		panic(err)
	}
	srv, err := gosmo.NewServer(context.Background(), pool)
	if err != nil {
		panic(err)
	}
	sc := &ServerConn{Opts: config.Connection{Server: server}, Server: srv}
	if len(registeredPeers) > 0 {
		sc.SetPeerCredentials(func(server string) (config.Connection, bool) {
			for _, peer := range registeredPeers {
				if config.InstanceKey(server) == config.InstanceKey(peer) {
					return config.Connection{Server: peer}, true
				}
			}
			return config.Connection{}, false
		})
	}
	return sc
}

func TestPeerDialsOnlyTheSavedDestinationAndCredentials(t *testing.T) {
	parent := newTestConn("primary.example.com")
	defer parent.Close()
	parent.Opts.User, parent.Opts.Password = "parent_login", "parent-password"
	saved := config.Connection{
		Server: "sql.production.example", Port: 1500,
		User: "replica_login", Password: "replica-password",
		Database: "SalesDB", Encrypt: config.EncryptMandatory,
		HostNameInCertificate: "sql.production.example",
	}
	parent.SetPeerCredentials(func(string) (config.Connection, bool) { return saved, true })
	orig := connectPeer
	t.Cleanup(func() { connectPeer = orig })
	var dialled []config.Connection
	wantErr := errors.New("saved peer login rejected")
	connectPeer = func(_ context.Context, opts config.Connection, _ Role) (*ServerConn, error) {
		dialled = append(dialled, opts)
		return nil, wantErr
	}

	peer, err := parent.Peer(context.Background(), "sql")
	if peer != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Peer = %v, %v; want the saved connection's failure", peer, err)
	}
	want := saved
	want.Database = ""
	if len(dialled) != 1 {
		t.Fatalf("dialled %d times; want exactly one attempt with the saved credentials", len(dialled))
	}
	if dialled[0] != want {
		t.Error("peer did not preserve the saved destination, credentials, and transport settings")
	}
}

func TestPeerRefusesUnregisteredDestinationsWithoutDialling(t *testing.T) {
	for _, resolver := range []string{"none", "miss", "empty destination"} {
		t.Run(resolver, func(t *testing.T) {
			parent := newTestConn("primary")
			defer parent.Close()
			parent.Opts.User, parent.Opts.Password = "parent_login", "parent-password"
			switch resolver {
			case "miss":
				parent.SetPeerCredentials(func(string) (config.Connection, bool) { return config.Connection{}, false })
			case "empty destination":
				parent.SetPeerCredentials(func(string) (config.Connection, bool) { return config.Connection{}, true })
			}
			orig := connectPeer
			t.Cleanup(func() { connectPeer = orig })
			dials := 0
			connectPeer = func(context.Context, config.Connection, Role) (*ServerConn, error) {
				dials++
				return nil, errors.New("unexpected dial")
			}
			peer, err := parent.Peer(context.Background(), "unregistered")
			if peer != nil || err == nil || !strings.Contains(err.Error(), "File > Connect") {
				t.Errorf("Peer = %v, %v; want an explicit registration instruction", peer, err)
			}
			if refused, ok := errors.AsType[*PeerNotRegisteredError](err); !ok || refused.Server != "unregistered" {
				t.Errorf("Peer error = %#v, want a *PeerNotRegisteredError naming the instance — the UI offers its Connect prompt on that type", err)
			}
			if dials != 0 {
				t.Errorf("dialled an unregistered destination %d times", dials)
			}
		})
	}
}

func TestPeerRegistrationIsCheckedBeforeCacheAndCanBeRepaired(t *testing.T) {
	parent := newTestConn("primary")
	defer parent.Close()
	registered := false
	saved := config.Connection{Server: "sql.production.example", Port: 1500}
	parent.SetPeerCredentials(func(string) (config.Connection, bool) { return saved, registered })
	orig := connectPeer
	t.Cleanup(func() { connectPeer = orig })
	dials := 0
	fail := true
	wantErr := errors.New("registered peer unavailable")
	connectPeer = func(_ context.Context, opts config.Connection, _ Role) (*ServerConn, error) {
		dials++
		if fail {
			return nil, wantErr
		}
		return newTestConn(opts.Server), nil
	}

	if _, err := parent.Peer(context.Background(), "sql"); err == nil || dials != 0 {
		t.Fatal("an unregistered alias was dialled")
	}
	registered = true
	if _, err := parent.Peer(context.Background(), "sql"); !errors.Is(err, wantErr) || dials != 1 {
		t.Fatal("registration was blocked by a cached refusal")
	}
	parent.ForgetPeerFailure(config.ConnectionAddress(saved))
	fail = false
	peer, err := parent.Peer(context.Background(), "sql")
	if err != nil || peer == nil || dials != 2 {
		t.Fatalf("retry after a direct connection = %v, %v; dials = %d", peer, err, dials)
	}
	if again, err := parent.Peer(context.Background(), saved.Server); err != nil || again != peer || dials != 2 {
		t.Fatal("aliases of the same registered destination did not share the cached peer")
	}
	registered = false
	if cached, err := parent.Peer(context.Background(), "sql"); err == nil || cached != nil || dials != 2 {
		t.Fatal("a cached peer bypassed a missing or ambiguous registration")
	}
}

// recordPeerFailure is recordPeerFailureLocked taking peerMu itself, for tests
// that seed the failure cache directly.
func (sc *ServerConn) recordPeerFailure(key string, err error) error {
	sc.peerMu.Lock()
	defer sc.peerMu.Unlock()
	return sc.recordPeerFailureLocked(key, err)
}

// closePeers closes each peer after releasing peerMu, while Peer's lookup asks
// the cached peer's IsOpen from another goroutine. closed is atomic, so this is
// race-free by construction.
//
// Meaningful only under -race.
func TestPeerLookupRacesDisconnect(t *testing.T) {
	for range 50 {
		sc := newTestConn("primary", "secondary")
		peer := newTestConn("secondary")
		sc.peers = map[string]*ServerConn{"secondary": peer}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			sc.Peer(context.Background(), "secondary")
		}()
		go func() {
			defer wg.Done()
			sc.Close()
		}()
		wg.Wait()
	}
}

// A named database must be openable or connect fails at ping ("Cannot open
// database %q that was requested by the login", verified live), and a secondary
// may not open the user's database, so Peer must not carry it. Peer's connect
// needs a real instance, so this tests the options it builds.
func TestPeerOptionsDropTheDatabase(t *testing.T) {
	sc := &ServerConn{}
	saved := config.Connection{
		Server: "secondary", Port: 1500, User: "sa", Password: "pw",
		Database: "SalesDB", TrustServerCertificate: true,
	}
	sc.SetPeerCredentials(func(string) (config.Connection, bool) { return saved, true })

	opts, err := sc.peerOptions("secondary")
	if err != nil {
		t.Fatal(err)
	}

	if opts.Database != "" {
		t.Errorf("peer options carry Database %q — a secondary that cannot open "+
			"it fails the connect outright", opts.Database)
	}
	if opts.Server != "secondary" {
		t.Errorf("peer options name server %q, want %q", opts.Server, "secondary")
	}
	// Everything else must survive from the registered peer.
	if opts.User != "sa" || opts.Password != "pw" || !opts.TrustServerCertificate {
		t.Errorf("peer options lost credentials or transport settings: %+v", opts)
	}
	if opts.Port != 1500 {
		t.Errorf("peer options use port %d, want the registered peer's 1500", opts.Port)
	}
}

// A replica needing a different login must use its own saved credentials. The
// requested name differs in spelling from the saved entry ("UBUSQL2\PROD" vs
// "ubusql2\prod,1433"), so skipping InstanceKey would miss.
func TestPeerOptionsUseTheInstancesOwnCredentials(t *testing.T) {
	parent := &ServerConn{Opts: config.Connection{
		Server: "ubusql1", Port: 1433, User: "sa", Password: "parent-pw",
		Database: "SalesDB", TrustServerCertificate: true,
	}}
	parent.SetPeerCredentials(func(server string) (config.Connection, bool) {
		if config.InstanceKey(server) == config.InstanceKey("ubusql2\\prod,1433") {
			return config.Connection{
				Server: "ubusql2.fritz.box\\prod", Port: 14330,
				User: "replica_login", Password: "replica-pw",
				Database: "OtherDB", Encrypt: config.EncryptStrict,
			}, true
		}
		return config.Connection{}, false
	})

	opts, err := parent.peerOptions("UBUSQL2\\PROD")
	if err != nil {
		t.Fatal(err)
	}

	if opts.User != "replica_login" || opts.Password != "replica-pw" {
		t.Errorf("peer options authenticate as %q/%q, want the instance's own saved login",
			opts.User, opts.Password)
	}
	if opts.Port != 14330 {
		t.Errorf("peer options use port %d, want the instance's own 14330 — the port half of the same limitation", opts.Port)
	}
	if opts.Encrypt != config.EncryptStrict {
		t.Error("peer options dropped the saved connection's Encrypt setting")
	}
	if opts.Server != "ubusql2.fritz.box\\prod" {
		t.Errorf("peer options name server %q, want the registered address", opts.Server)
	}
	// The database is blanked here too; the saved one is just as unopenable on
	// a secondary.
	if opts.Database != "" {
		t.Errorf("peer options carry Database %q from the saved connection", opts.Database)
	}
}

// A port embedded in the registered destination survives an alias lookup.
func TestPeerOptionsKeepAPortWrittenInTheSavedServer(t *testing.T) {
	parent := &ServerConn{Opts: config.Connection{Server: "ubusql1", User: "sa", Password: "p"}}
	parent.SetPeerCredentials(func(server string) (config.Connection, bool) {
		if config.InstanceKey(server) == config.InstanceKey(`win10cli\sql2017`) {
			return config.Connection{Server: `win10cli\SQL2017,55253`, User: "sa", Password: "p"}, true
		}
		return config.Connection{}, false
	})

	opts, err := parent.peerOptions(`WIN10CLI\SQL2017`)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Server != `win10cli\SQL2017,55253` {
		t.Fatalf("peer server = %q, want the registered address including its port", opts.Server)
	}
	dsn, err := BuildConnectionString(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "@win10cli:55253/SQL2017?") {
		t.Errorf("peer dials %s, want win10cli:55253/SQL2017 (no SQL Browser lookup)", dsn)
	}
}

func TestPeerOptionsRefuseAResolverMiss(t *testing.T) {
	parent := &ServerConn{Opts: config.Connection{
		Server: "ubusql1", Port: 1433, User: "sa", Password: "parent-pw",
		Database: "SalesDB", TrustServerCertificate: true,
	}}
	parent.SetPeerCredentials(func(string) (config.Connection, bool) {
		return config.Connection{}, false
	})

	opts, err := parent.peerOptions("ubusql3")
	if err == nil || opts != (config.Connection{}) {
		t.Fatal("a resolver miss exposed the parent's connection options")
	}
}

func TestPeerCredentialsRefuseANilResolver(t *testing.T) {
	parent := &ServerConn{Opts: config.Connection{Server: "ubusql1", User: "sa", Password: "pw"}}
	if opts, err := parent.peerOptions("ubusql2"); err == nil || opts != (config.Connection{}) {
		t.Fatal("a missing resolver exposed the parent's connection options")
	}
}

// Peer hands the parent's resolver to each peer (peerCredentials, then
// SetPeerCredentials) so the next hop resolves through the same table rather
// than using the primary's login. Peer's own call needs a real instance, so the
// wiring is verified live.
func TestPeerCredentialsAreInheritedByAPeer(t *testing.T) {
	parent := &ServerConn{Opts: config.Connection{Server: "ubusql1", User: "sa", Password: "parent-pw"}}
	parent.SetPeerCredentials(func(server string) (config.Connection, bool) {
		if config.InstanceKey(server) == config.InstanceKey("ubusql3") {
			return config.Connection{Server: "ubusql3", User: "third_login", Password: "third-pw"}, true
		}
		return config.Connection{}, false
	})

	// Exactly what Peer does to the connection it just opened.
	peer := &ServerConn{Opts: config.Connection{Server: "ubusql2", User: "sa", Password: "parent-pw"}}
	peer.SetPeerCredentials(parent.peerCredentials())

	got, err := peer.peerOptions("ubusql3")
	if err != nil {
		t.Fatal(err)
	}
	if got.User != "third_login" {
		t.Errorf("a peer resolved ubusql3 as %q, want the resolver's %q — the table did not reach the second hop",
			got.User, "third_login")
	}
}

// A blackholed instance costs the whole connect timeout, so a cached failure
// must be returned without dialling.
func TestPeerReturnsACachedFailureWithoutDialling(t *testing.T) {
	sc := newTestConn("ubusql1", "ubusql2")
	defer sc.Close()

	want := errors.New("dial tcp 192.168.178.98:1433: i/o timeout")
	sc.recordPeerFailure(config.InstanceKey("ubusql2"), want)

	// A different spelling than recorded: both must agree through InstanceKey.
	start := time.Now()
	peer, err := sc.Peer(context.Background(), "UBUSQL2,1433")
	if !errors.Is(err, want) {
		t.Fatalf("Peer = %v, %v; want the cached failure %v", peer, err, want)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the cached failure took %v to answer; that is a dial, not a cache hit", elapsed)
	}
}

// Entries expire, or a recovered primary stays unreachable. An aged entry means
// a real dial, whose failure is the dial's own.
func TestPeerRetriesOnceACachedFailureHasExpired(t *testing.T) {
	// Loopback port 1 refuses immediately.
	sc := newTestConn("ubusql1", "127.0.0.1:1")
	defer sc.Close()

	key := config.InstanceKey("127.0.0.1:1")
	cached := errors.New("dial tcp 192.168.178.98:1433: i/o timeout")
	sc.recordPeerFailure(key, cached)
	sc.peerMu.Lock()
	f := sc.peerFails[key]
	f.at = time.Now().Add(-peerFailureTTL - time.Second)
	sc.peerFails[key] = f
	sc.peerMu.Unlock()

	_, err := sc.Peer(context.Background(), "127.0.0.1:1")
	if err == nil {
		t.Fatal("something answered on 127.0.0.1:1; the test cannot tell a retry from a cache hit")
	}
	if errors.Is(err, cached) {
		t.Errorf("Peer answered with the expired failure %v; the entry outlived peerFailureTTL", cached)
	}
	// The fresh failure replaces it.
	sc.peerMu.Lock()
	fresh, ok := sc.peerFails[key]
	sc.peerMu.Unlock()
	if !ok || errors.Is(fresh.err, cached) {
		t.Error("the retry did not record its own failure; the expired entry is still what answers")
	}
}

// ForgetPeerFailure makes the next call dial rather than replay the entry until
// the TTL.
func TestForgetPeerFailureDropsTheEntry(t *testing.T) {
	// Loopback port 1 refuses immediately.
	sc := newTestConn("ubusql1", "127.0.0.1:1")
	defer sc.Close()

	cached := errors.New("dial tcp 192.168.178.98:1433: i/o timeout")
	sc.recordPeerFailure(config.InstanceKey("127.0.0.1:1"), cached)

	// A different spelling on purpose: Connect holds the typed name, the cache
	// the catalog's; InstanceKey reconciles them.
	sc.ForgetPeerFailure("127.0.0.1,1")

	_, err := sc.Peer(context.Background(), "127.0.0.1:1")
	if err == nil {
		t.Fatal("something answered on 127.0.0.1:1; the test cannot tell a dial from a cache hit")
	}
	if errors.Is(err, cached) {
		t.Errorf("Peer answered with the forgotten failure %v; ForgetPeerFailure did not drop it", cached)
	}
}

// Forgetting one instance must not clear others; a connect to one replica says
// nothing about another.
func TestForgetPeerFailureLeavesOtherInstances(t *testing.T) {
	sc := newTestConn("ubusql1", "ubusql2")
	defer sc.Close()

	want := errors.New("dial tcp 192.168.178.98:1433: i/o timeout")
	sc.recordPeerFailure(config.InstanceKey("ubusql2"), want)
	sc.recordPeerFailure(config.InstanceKey("ubusql3"), errors.New("other"))

	sc.ForgetPeerFailure("ubusql3")

	if _, err := sc.Peer(context.Background(), "ubusql2"); !errors.Is(err, want) {
		t.Errorf("Peer(ubusql2) = %v; want the still-cached failure %v", err, want)
	}
}

// Reads chain, so a third instance's failure is recorded on the primary's
// connection. Two instances that opened each other form a cycle; without seen
// this hangs.
func TestForgetPeerFailuresReachCachedPeers(t *testing.T) {
	sc := newTestConn("ubusql1")
	defer sc.Close()
	primary := newTestConn("ubusql2")
	defer primary.Close()

	sc.peers = map[string]*ServerConn{"ubusql2": primary}
	primary.peers = map[string]*ServerConn{"ubusql1": sc}
	primary.recordPeerFailure(config.InstanceKey("ubusql3"), errors.New("dial tcp: i/o timeout"))

	sc.ForgetPeerFailures()

	primary.peerMu.Lock()
	n := len(primary.peerFails)
	primary.peerMu.Unlock()
	if n != 0 {
		t.Errorf("the peer still holds %d cached failure(s); a chained read stays blackholed", n)
	}
}

// holdPeerDials replaces connectPeer with a dial that counts itself and blocks
// until release is closed or its ctx ends, then answers with a fresh
// connection. Restored on cleanup.
func holdPeerDials(t *testing.T) (dials *atomic.Int32, release chan struct{}) {
	t.Helper()
	dials = new(atomic.Int32)
	release = make(chan struct{})
	orig := connectPeer
	connectPeer = func(ctx context.Context, opts config.Connection, _ Role) (*ServerConn, error) {
		dials.Add(1)
		select {
		case <-release:
			return newTestConn(opts.Server), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	t.Cleanup(func() { connectPeer = orig })
	return dials, release
}

// waitFor polls cond until it holds, failing the test after 5s rather than
// letting a broken Peer hang the run.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitForPeerWaiters blocks until n callers have joined key's in-flight dial.
func waitForPeerWaiters(t *testing.T, sc *ServerConn, key string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sc.peerMu.Lock()
		d := sc.peerDials[key]
		joined := d != nil && d.waiters >= n
		sc.peerMu.Unlock()
		if joined {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%d callers never joined the dial to %s", n, key)
}

// Three folders of one group asking for the same primary at once dial it once
// and share the connection.
func TestConcurrentPeerCallersShareOneDial(t *testing.T) {
	dials, release := holdPeerDials(t)
	sc := newTestConn("ubusql1", "ubusql2")
	defer sc.Close()

	const callers = 3
	got := make([]*ServerConn, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			p, err := sc.Peer(context.Background(), "ubusql2")
			if err != nil {
				t.Errorf("Peer: %v", err)
			}
			got[i] = p
		})
	}
	waitForPeerWaiters(t, sc, "ubusql2", callers-1)
	close(release)
	wg.Wait()

	if n := dials.Load(); n != 1 {
		t.Errorf("%d dials for one instance; concurrent callers must share one", n)
	}
	for i, p := range got {
		if p == nil || p != got[0] {
			t.Errorf("caller %d got %p, caller 0 got %p; all must share the one connection", i, p, got[0])
		}
	}
}

// A waiter is bounded by its own ctx, not the dial's: a superseded load
// returns at once rather than waiting out someone else's connect.
func TestPeerWaiterReturnsWhenItsOwnContextEnds(t *testing.T) {
	_, release := holdPeerDials(t)
	sc := newTestConn("ubusql1", "ubusql2")
	defer sc.Close()

	var dialler sync.WaitGroup
	defer dialler.Wait()
	defer close(release)
	dialler.Go(func() { sc.Peer(context.Background(), "ubusql2") })
	waitFor(t, "the first dial", func() bool {
		sc.peerMu.Lock()
		defer sc.peerMu.Unlock()
		_, inFlight := sc.peerDials["ubusql2"]
		return inFlight
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := sc.Peer(ctx, "ubusql2")
		done <- err
	}()
	waitForPeerWaiters(t, sc, "ubusql2", 1)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Peer = %v; want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter outlived its own context")
	}
}

// The dialling caller's ctx bounds the connect itself, and a cancelled dial is
// not cached as the instance's failure: a waiter still wanting an answer dials
// again, and succeeds.
func TestAbandonedPeerDialIsRetriedByAWaiter(t *testing.T) {
	dials, release := holdPeerDials(t)
	sc := newTestConn("ubusql1", "ubusql2")
	defer sc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := sc.Peer(ctx, "ubusql2")
		first <- err
	}()
	waitFor(t, "the first dial", func() bool { return dials.Load() > 0 })

	second := make(chan *ServerConn, 1)
	go func() {
		p, err := sc.Peer(context.Background(), "ubusql2")
		if err != nil {
			t.Errorf("waiter: %v", err)
		}
		second <- p
	}()
	waitForPeerWaiters(t, sc, "ubusql2", 1)
	cancel()
	select {
	case err := <-first:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("dialling caller = %v; want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the dialling caller's ctx did not end its connect")
	}
	close(release)
	select {
	case p := <-second:
		if p == nil {
			t.Fatal("the waiter got no connection after the abandoned dial")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never got an answer after the abandoned dial")
	}
	if n := dials.Load(); n != 2 {
		t.Errorf("%d dials; the waiter must dial once more after the abandoned one", n)
	}

	sc.peerMu.Lock()
	_, failed := sc.peerFails["ubusql2"]
	sc.peerMu.Unlock()
	if failed {
		t.Error("the cancelled dial was cached as the instance's failure")
	}
}

// A closed parent answers ErrServerConnClosed whatever the cache holds: its own
// instance, a live cached peer, a cached failure. The error is still a
// cancellation, which UI callers treat as "superseded".
func TestPeerOfAClosedParentRefuses(t *testing.T) {
	for _, server := range []string{"primary", "secondary", "failed"} {
		sc := newTestConn("primary")
		peer := newTestConn("secondary")
		defer peer.Close()
		sc.peers = map[string]*ServerConn{"secondary": peer}
		sc.recordPeerFailure(config.InstanceKey("failed"), errors.New("i/o timeout"))
		sc.closed.Store(true) // closed, but closePeers not yet run: Close mid-way

		got, err := sc.Peer(context.Background(), server)
		if got != nil || !errors.Is(err, ErrServerConnClosed) || !errors.Is(err, context.Canceled) {
			t.Errorf("Peer(%q) on a closed parent = %v, %v; want ErrServerConnClosed", server, got, err)
		}
	}
}
