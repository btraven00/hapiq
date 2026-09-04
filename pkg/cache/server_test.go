package cache_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// servePopulatedCache returns a test server fronting a cache that already holds
// content under originURL, plus the content's sha256.
func servePopulatedCache(t *testing.T, cfg cache.ServerConfig, originURL string, content []byte) (*httptest.Server, string) {
	t.Helper()

	c := openTestCache(t)
	tmpPath, wantHash := writeTmp(t, c, content)
	if err := c.Put(context.Background(), originURL, tmpPath, wantHash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	srv := httptest.NewServer(cache.NewServer(c, cfg).Handler())
	t.Cleanup(srv.Close)
	return srv, wantHash
}

func TestServerResolveAndBlob(t *testing.T) {
	const origin = "https://example.org/data.h5ad"
	content := []byte("some dataset bytes")
	srv, wantHash := servePopulatedCache(t, cache.ServerConfig{}, origin, content)

	resp, err := http.Get(srv.URL + "/v1/resolve?url=" + url.QueryEscape(origin))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.SHA256 != wantHash {
		t.Errorf("resolve sha256 = %s, want %s", body.SHA256, wantHash)
	}
	if body.Size != int64(len(content)) {
		t.Errorf("resolve size = %d, want %d", body.Size, len(content))
	}

	blobResp, err := http.Get(srv.URL + "/v1/blob/" + wantHash)
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	defer blobResp.Body.Close()

	got := sha256.New()
	if _, err := io.Copy(got, blobResp.Body); err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got.Sum(nil)) != wantHash {
		t.Error("blob body does not hash to the requested key")
	}
	if want := `"sha256:` + wantHash + `"`; blobResp.Header.Get("ETag") != want {
		t.Errorf("ETag = %q, want %q", blobResp.Header.Get("ETag"), want)
	}
}

// TestServerRejectsBadHash is the security-relevant case: the hash lands in a
// filesystem path, so anything that is not plain lowercase hex must be refused
// before it gets there.
func TestServerRejectsBadHash(t *testing.T) {
	srv, _ := servePopulatedCache(t, cache.ServerConfig{}, "https://example.org/x", []byte("x"))

	for _, bad := range []string{
		"../../../../etc/passwd",
		"..%2f..%2fetc%2fpasswd",
		"ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789", // uppercase
		"short",
		"zz23456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", // non-hex
	} {
		resp, err := http.Get(srv.URL + "/v1/blob/" + bad)
		if err != nil {
			t.Fatalf("%s: %v", bad, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("blob %q returned 200, want a rejection", bad)
		}
	}
}

func TestServerTokenRequired(t *testing.T) {
	const origin = "https://example.org/data"
	srv, _ := servePopulatedCache(t, cache.ServerConfig{Token: "s3cret"}, origin, []byte("bytes"))

	resp, err := http.Get(srv.URL + "/v1/resolve?url=" + url.QueryEscape(origin))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated resolve = %d, want 401", resp.StatusCode)
	}

	// healthz stays open so a load balancer needs no credentials.
	h, err := http.Get(srv.URL + "/v1/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	h.Body.Close()
	if h.StatusCode != http.StatusOK {
		t.Errorf("healthz = %d, want 200", h.StatusCode)
	}
}

// TestIntroducerRemembersCallers covers the whole introducer protocol: a node
// that identifies itself in a request header is handed out to later callers.
func TestIntroducerRemembersCallers(t *testing.T) {
	srv, _ := servePopulatedCache(t, cache.ServerConfig{}, "https://example.org/x", []byte("x"))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/healthz", http.NoBody)
	req.Header.Set("X-Hapiq-Self", "http://10.1.2.3:7777")
	// healthz is unguarded, so use resolve to exercise registration.
	req.URL.Path = "/v1/resolve"
	req.URL.RawQuery = "url=" + url.QueryEscape("https://example.org/x")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("announce: %v", err)
	}
	resp.Body.Close()

	peers := cache.NewPeers(cache.ServerConfig{})
	peers.Bootstrap(context.Background(), []string{srv.URL})

	var found bool
	for _, u := range peers.URLs() {
		if u == "http://10.1.2.3:7777" {
			found = true
		}
	}
	if !found {
		t.Errorf("introducer did not hand back the announced peer; got %v", peers.URLs())
	}
}

// TestFetchFromPeer is the end-to-end path: node B has no local copy, node A
// does, and B ends up with verified bytes without touching the origin (which
// does not exist here — the origin URL is unroutable).
func TestFetchFromPeer(t *testing.T) {
	const origin = "https://origin.invalid/dataset.h5ad"
	content := []byte("payload that only the peer has")
	srv, wantHash := servePopulatedCache(t, cache.ServerConfig{}, origin, content)

	peers := cache.NewPeers(cache.ServerConfig{Peers: []string{srv.URL}})
	if peers.Len() != 1 {
		t.Fatalf("peer not registered: %v", peers.URLs())
	}

	dest := filepath.Join(t.TempDir(), "dataset.h5ad")
	ctx := cache.WithPeers(context.Background(), peers)

	res, err := common.Fetch(ctx, origin, dest, common.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch via peer: %v", err)
	}
	if res.PeerURL != srv.URL {
		t.Errorf("PeerURL = %q, want %q", res.PeerURL, srv.URL)
	}
	if res.SHA256 != wantHash {
		t.Errorf("SHA256 = %s, want %s", res.SHA256, wantHash)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("dest content = %q, want %q", got, content)
	}
}

// TestPeerServingWrongBytesIsRejected is why peer selection needs no trust: a
// peer that lies about its content is caught by the hash and the download falls
// through to the origin.
func TestPeerServingWrongBytesIsRejected(t *testing.T) {
	honest := []byte("the real dataset")
	wantHash := sha256.Sum256(honest)
	hashHex := hex.EncodeToString(wantHash[:])

	// A peer that advertises the correct hash but serves something else.
	liar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{"sha256": hashHex, "size": len(honest)})
		default:
			_, _ = w.Write([]byte("malicious substitute"))
		}
	}))
	defer liar.Close()

	// The origin does exist in this test, and must be what we end up with.
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(honest)
	}))
	defer svc.Close()

	peers := cache.NewPeers(cache.ServerConfig{Peers: []string{liar.URL}})
	dest := filepath.Join(t.TempDir(), "dataset.h5ad")
	ctx := cache.WithPeers(context.Background(), peers)

	res, err := common.Fetch(ctx, svc.URL, dest, common.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.PeerURL != "" {
		t.Errorf("lying peer was accepted (PeerURL=%q)", res.PeerURL)
	}
	if res.SHA256 != hashHex {
		t.Errorf("did not fall back to origin: got hash %s, want %s", res.SHA256, hashHex)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(honest) {
		t.Errorf("dest content = %q, want the origin's bytes", got)
	}
}

// TestUnreachablePeerIsDropped: mDNS advertises on every interface, so a host
// with docker bridges offers several addresses of which one works. Without
// pruning, each dead address would cost a dial timeout on every file.
func TestUnreachablePeerIsDropped(t *testing.T) {
	const origin = "https://example.org/data"
	live, wantHash := servePopulatedCache(t, cache.ServerConfig{}, origin, []byte("bytes"))

	// A port nobody is listening on: connection refused, no timeout wait.
	dead := "http://127.0.0.1:1"
	peers := cache.NewPeers(cache.ServerConfig{Peers: []string{dead, live.URL}})
	if peers.Len() != 2 {
		t.Fatalf("setup: peers = %v", peers.URLs())
	}

	hit, ok := peers.Resolve(context.Background(), origin)
	if !ok || hit.SHA256 != wantHash {
		t.Fatalf("resolve past the dead peer failed: ok=%v hit=%+v", ok, hit)
	}

	if got := peers.URLs(); len(got) != 1 || got[0] != live.URL {
		t.Errorf("after resolve, peers = %v, want only %s", got, live.URL)
	}

	// A live peer that simply lacks the file must NOT be forgotten.
	if _, ok := peers.Resolve(context.Background(), "https://example.org/absent"); ok {
		t.Error("resolve of an uncached URL reported a hit")
	}
	if got := peers.URLs(); len(got) != 1 {
		t.Errorf("live peer dropped on a 404: peers = %v", got)
	}
}

// TestPeersRoundRobin: with several static peers, consecutive resolves must
// start from different peers, otherwise the first entry serves every file and
// the rest of the set is dead weight. Asserted on who each peer actually
// contacts first, not on internal state.
func TestPeersRoundRobin(t *testing.T) {
	const origin = "https://example.org/data"

	var mu sync.Mutex
	var contacts []int // peer indices, in the order contacted this round

	var urls []string
	for i := 0; i < 3; i++ {
		idx := i
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			contacts = append(contacts, idx)
			mu.Unlock()
			// Nobody has the file, so each resolve walks the whole set and we
			// see the full contact order.
			http.Error(w, "not cached", http.StatusNotFound)
		}))
		defer srv.Close()
		urls = append(urls, srv.URL)
	}

	peers := cache.NewPeers(cache.ServerConfig{Peers: urls})

	wentFirst := make(map[int]int)
	for round := 0; round < 9; round++ {
		mu.Lock()
		contacts = nil
		mu.Unlock()

		if _, ok := peers.Resolve(context.Background(), origin); ok {
			t.Fatal("resolve reported a hit when no peer has the file")
		}

		mu.Lock()
		got := append([]int(nil), contacts...)
		mu.Unlock()

		if len(got) != 3 {
			t.Fatalf("round %d contacted %d peers, want all 3: %v", round, len(got), got)
		}
		wentFirst[got[0]]++
	}

	if len(wentFirst) != 3 {
		t.Errorf("only %d of 3 peers ever went first: %v", len(wentFirst), wentFirst)
	}
	for idx, n := range wentFirst {
		if n != 3 {
			t.Errorf("peer %d went first %d times over 9 rounds, want an even 3", idx, n)
		}
	}
}

// TestPeerTimeoutIsConfigurable guards the knob that matters for high-latency
// links: with the LAN default, a slow peer is pruned and never used again.
func TestPeerTimeoutIsConfigurable(t *testing.T) {
	const origin = "https://example.org/data"
	c := openTestCache(t)
	tmpPath, wantHash := writeTmp(t, c, []byte("bytes"))
	if err := c.Put(context.Background(), origin, tmpPath, wantHash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	inner := cache.NewServer(c, cache.ServerConfig{}).Handler()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		inner.ServeHTTP(w, r)
	}))
	defer slow.Close()

	tight := cache.NewPeers(cache.ServerConfig{Peers: []string{slow.URL}, Timeout: 20 * time.Millisecond})
	if _, ok := tight.Resolve(context.Background(), origin); ok {
		t.Error("resolve succeeded despite a timeout shorter than the peer's latency")
	}

	patient := cache.NewPeers(cache.ServerConfig{Peers: []string{slow.URL}, Timeout: 3 * time.Second})
	hit, ok := patient.Resolve(context.Background(), origin)
	if !ok || hit.SHA256 != wantHash {
		t.Errorf("resolve with a generous timeout failed: ok=%v hit=%+v", ok, hit)
	}
}

// TestMDNSRoundTrip exercises the zeroconf advertise/browse pair against a real
// multicast socket. Skipped where the sandbox has no usable multicast.
func TestMDNSRoundTrip(t *testing.T) {
	c := openTestCache(t)
	srv := cache.NewServer(c, cache.ServerConfig{Listen: "0.0.0.0:7799"})

	stop, err := srv.AdvertiseMDNS()
	if err != nil {
		t.Skipf("mdns unavailable in this environment: %v", err)
	}
	defer stop()

	peers := cache.NewPeers(cache.ServerConfig{})
	peers.DiscoverMDNS(context.Background())

	var found bool
	for _, u := range peers.URLs() {
		if strings.HasSuffix(u, ":7799") {
			found = true
		}
	}
	if !found {
		t.Skipf("no _hapiq._tcp answer on this host (multicast blocked?); discovered %v", peers.URLs())
	}
}
