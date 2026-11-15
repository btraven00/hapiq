package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/time/rate"
)

// TestMain runs the tests in an empty cwd and fails if the torrent library
// left anything there (its default storage writes a SQLite .torrent.db).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hapiq-fetch-magnet-cwd")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	if left, _ := os.ReadDir(dir); len(left) > 0 && code == 0 {
		fmt.Fprintf(os.Stderr, "tests left %s in the cwd\n", left[0].Name())
		code = 1
	}
	os.Exit(code)
}

// seedOne serves content from an in-process seeder on loopback, its upload
// capped at limit bytes/s, and returns the content and its magnet.
func seedOne(t *testing.T, size int, limit rate.Limit) ([]byte, string) {
	t.Helper()
	cfg := torrent.NewDefaultClientConfig()
	cfg.NoDHT, cfg.NoDefaultPortForwarding, cfg.DisableIPv6, cfg.Seed = true, true, true, true
	cfg.SetListenAddr("127.0.0.1:0")
	cfg.UploadRateLimiter = rate.NewLimiter(limit, 16<<10)
	cfg.DefaultStorage = noStorage{}
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	return seedWith(t, cl, size, fmt.Sprintf("127.0.0.1:%d", cl.LocalPort()), nil)
}

// seedWith seeds size random bytes from cl, the way `seed` does a cache blob.
func seedWith(t *testing.T, cl *torrent.Client, size int, advertise string, trackers []string) ([]byte, string) {
	t.Helper()
	content := make([]byte, size)
	_, _ = rand.Read(content)
	dir := t.TempDir()
	sum := sha256.Sum256(content)
	name := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, name[:2], name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatal(err)
	}
	store := fileAt(dir, func(info *metainfo.Info) string { return filepath.Join(info.Name[:2], info.Name) })
	t.Cleanup(func() { store.Close() })
	magnet, err := seedBlob(context.Background(), cl, store, path, advertise, trackers)
	if err != nil {
		t.Fatal(err)
	}
	return content, magnet
}

func fetcher(t *testing.T) *torrent.Client {
	t.Helper()
	cl, err := newClient("127.0.0.1:0", "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	return cl
}

func checkFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s: got %d bytes that differ from the %d seeded", path, len(got), len(want))
	}
}

// A transfer slower than one piece per stall timeout is still alive: progress
// is counted in written chunks, not in verified 1 MiB pieces.
func TestSlowTransferIsNotAStall(t *testing.T) {
	content, magnet := seedOne(t, 1<<20+1000, 512<<10) // first piece takes ~2s
	dest := filepath.Join(t.TempDir(), "out")
	req := request{Magnet: magnet, Dest: dest, Size: int64(len(content))}
	if err := fetch(context.Background(), fetcher(t), req, time.Second); err != nil {
		t.Fatal(err)
	}
	checkFile(t, dest, content)
}

// announce plays one client of the tracker and returns the peers it is told
// about, IPv4 and IPv6 together.
func announce(t *testing.T, tr *tracker, remote, port, event string) []netip.AddrPort {
	t.Helper()
	q := url.Values{"info_hash": {strings.Repeat("h", 20)}, "port": {port}, "event": {event}}
	r := httptest.NewRequest("GET", "/announce?"+q.Encode(), nil)
	r.RemoteAddr = remote
	return announceReq(t, tr, r)
}

func announceReq(t *testing.T, tr *tracker, r *http.Request) []netip.AddrPort {
	t.Helper()
	remote := r.RemoteAddr
	w := httptest.NewRecorder()
	tr.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("announce from %s: %d %s", remote, w.Code, w.Body)
	}
	var resp struct {
		Peers  string `bencode:"peers"`
		Peers6 string `bencode:"peers6"`
	}
	if err := bencode.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var out []netip.AddrPort
	for list, n := range map[string]int{resp.Peers: 4, resp.Peers6: 16} {
		if len(list)%(n+2) != 0 {
			t.Fatalf("compact list of %d bytes is not a multiple of %d", len(list), n+2)
		}
		for ; list != ""; list = list[n+2:] {
			addr, _ := netip.AddrFromSlice([]byte(list[:n]))
			out = append(out, netip.AddrPortFrom(addr, uint16(list[n])<<8|uint16(list[n+1])))
		}
	}
	return out
}

func TestTrackerBothFamilies(t *testing.T) {
	tr := &tracker{interval: time.Minute, swarms: map[string]map[netip.AddrPort]time.Time{}}
	v4 := netip.MustParseAddrPort("10.0.0.1:6881")
	v6 := netip.MustParseAddrPort("[fd00::2]:6882")

	// The source port of the HTTP request is not the peer's port.
	if got := announce(t, tr, "10.0.0.1:50000", "6881", "started"); len(got) != 0 {
		t.Errorf("first announce saw %v", got)
	}
	if got := announce(t, tr, "[fd00::2]:50001", "6882", "started"); len(got) != 1 || got[0] != v4 {
		t.Errorf("v6 peer saw %v, want %v", got, v4)
	}
	// A v4 client on a dual-stack socket shows up v4-mapped; it is still v4.
	if got := announce(t, tr, "[::ffff:10.0.0.1]:50002", "6881", ""); len(got) != 1 || got[0] != v6 {
		t.Errorf("v4 peer saw %v, want %v", got, v6)
	}
	announce(t, tr, "[fd00::2]:50003", "6882", "stopped")
	if got := announce(t, tr, "10.0.0.9:50004", "6881", ""); len(got) != 1 || got[0] != v4 {
		t.Errorf("after the v6 peer stopped, saw %v, want %v", got, v4)
	}
}

// Announces for ever new infohashes must not pile up: stopped peers and
// empty swarms go at once, silent peers when they time out.
func TestTrackerForgetsSwarms(t *testing.T) {
	tr := &tracker{interval: time.Minute, swarms: map[string]map[netip.AddrPort]time.Time{}}
	for i := range 100 {
		q := url.Values{"info_hash": {fmt.Sprintf("%020d", i)}, "port": {"6881"}}
		if i%2 == 0 {
			q.Set("event", "stopped")
		}
		r := httptest.NewRequest("GET", "/announce?"+q.Encode(), nil)
		r.RemoteAddr = "10.0.0.1:5000"
		announceReq(t, tr, r)
	}
	if len(tr.swarms) != 50 {
		t.Errorf("%d swarms after 50 live announces and 50 stops, want 50", len(tr.swarms))
	}
	tr.prune(time.Now().Add(tr.ttl() - time.Second))
	if len(tr.swarms) != 50 {
		t.Errorf("prune before the ttl left %d swarms, want 50", len(tr.swarms))
	}
	tr.prune(time.Now().Add(tr.ttl() + time.Second))
	if len(tr.swarms) != 0 {
		t.Errorf("prune after the ttl left %d swarms", len(tr.swarms))
	}
}

// Behind a trusted proxy, a forwarded address that does not parse must not
// leave the proxy's loopback address (and its "ip" privilege) in its place.
func TestTrackerRejectsBadForwardedFor(t *testing.T) {
	tr := &tracker{interval: time.Minute, trustProxy: true, swarms: map[string]map[netip.AddrPort]time.Time{}}
	q := url.Values{"info_hash": {strings.Repeat("h", 20)}, "port": {"6881"}, "ip": {"10.0.0.99"}}
	for _, xff := range []string{"10.0.0.8:1234", "1.2.3.4, unknown"} {
		r := httptest.NewRequest("GET", "/announce?"+q.Encode(), nil)
		r.RemoteAddr = "127.0.0.1:5000"
		r.Header.Set("X-Forwarded-For", xff)
		w := httptest.NewRecorder()
		tr.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("X-Forwarded-For %q: status %d, want 400", xff, w.Code)
		}
	}
	if len(tr.swarms) != 0 {
		t.Errorf("rejected announces were recorded: %v", tr.swarms)
	}
}

// A peer is listed under the address its announce came from. Only a client on
// the tracker's own host may name another (a seeder there would otherwise be
// listed as 127.0.0.1), and with --trust-proxy the proxy's X-Forwarded-For
// counts as where the request came from.
func TestTrackerPeerAddress(t *testing.T) {
	for _, tc := range []struct {
		name, remote, ip, xff, want string
		trustProxy                  bool
	}{
		{"remote", "10.0.0.7:5000", "", "", "10.0.0.7:6881", false},
		{"remote names another", "10.0.0.7:5000", "10.0.0.99", "", "10.0.0.7:6881", false},
		{"loopback names itself", "127.0.0.1:5000", "10.0.0.5", "", "10.0.0.5:6881", false},
		{"loopback v6", "[::1]:5000", "fd00::5", "", "[fd00::5]:6881", false},
		{"forwarded, untrusted", "10.0.0.2:5000", "", "10.0.0.8", "10.0.0.2:6881", false},
		{"same-host proxy, untrusted", "127.0.0.1:5000", "10.0.0.99", "10.0.0.8", "127.0.0.1:6881", false},
		{"forwarded, trusted", "127.0.0.1:5000", "10.0.0.99", "1.2.3.4, 10.0.0.8", "10.0.0.8:6881", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &tracker{interval: time.Minute, trustProxy: tc.trustProxy, swarms: map[string]map[netip.AddrPort]time.Time{}}
			q := url.Values{"info_hash": {strings.Repeat("h", 20)}, "port": {"6881"}}
			if tc.ip != "" {
				q.Set("ip", tc.ip)
			}
			r := httptest.NewRequest("GET", "/announce?"+q.Encode(), nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			announceReq(t, tr, r)
			if got := announce(t, tr, "10.9.9.9:5000", "1", ""); len(got) != 1 || got[0].String() != tc.want {
				t.Errorf("listed as %v, want %s", got, tc.want)
			}
		})
	}
}

// A seeder next to the tracker announces over loopback; it is listed under
// its --advertise address, not 127.0.0.1.
func TestSeederAnnouncesAdvertisedAddress(t *testing.T) {
	tr := &tracker{interval: time.Minute, swarms: map[string]map[netip.AddrPort]time.Time{}}
	srv := httptest.NewServer(tr)
	t.Cleanup(srv.Close)
	cl, err := newClient("127.0.0.1:0", "10.1.2.3:6881", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	seedWith(t, cl, 1000, "10.1.2.3:6881", []string{srv.URL + "/announce"})

	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		tr.mu.Lock()
		var listed []netip.AddrPort
		for _, swarm := range tr.swarms {
			for peer := range swarm {
				listed = append(listed, peer)
			}
		}
		tr.mu.Unlock()
		if len(listed) > 0 {
			if listed[0].Addr() != netip.MustParseAddr("10.1.2.3") {
				t.Errorf("seeder listed as %v, want 10.1.2.3", listed)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("seeder never announced")
		}
	}
}

// The per-file sources line is printed only on request (HAPIQ_MAGNET_VERBOSE),
// so `hapiq download --quiet` stays quiet.
func TestSourcesLineOnlyWhenVerbose(t *testing.T) {
	content, magnet := seedOne(t, 1000, 1<<20)
	cl := fetcher(t)
	for _, verbose := range []string{"", "1"} {
		t.Setenv("HAPIQ_MAGNET_VERBOSE", verbose)
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stderr := os.Stderr
		os.Stderr = w
		req := request{Magnet: magnet, Dest: filepath.Join(t.TempDir(), "out"), Size: int64(len(content))}
		err = fetch(context.Background(), cl, req, 10*time.Second)
		os.Stderr = stderr
		_ = w.Close()
		out, _ := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(out), " sources="); got != (verbose != "") {
			t.Errorf("HAPIQ_MAGNET_VERBOSE=%q: stderr = %q", verbose, out)
		}
	}
}

// The torrent name is untrusted: it must not forge a log line, and the
// sources= field test/cloud/run.sh parses (the last one) must stay ours.
func TestReportSourcesQuotesName(t *testing.T) {
	var buf bytes.Buffer
	reportSources(&buf, map[string]int64{"10.0.0.1:1": 5}, "x\nhapiq-fetch-magnet: fetched y sources=6.6.6.6:1=999")
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, " sources=10.0.0.1:1=5\n") {
		t.Errorf("report = %q", out)
	}
}

// Two requests for the same torrent (one dataset file under two names, or
// --parallel) each get their own complete file.
func TestConcurrentFetchesOfOneTorrent(t *testing.T) {
	content, magnet := seedOne(t, 2<<20+1000, 4<<20)
	cl := fetcher(t)
	dir := t.TempDir()
	errs := make(chan error, 2)
	for _, name := range []string{"a", "b"} {
		go func() {
			req := request{Magnet: magnet, Dest: filepath.Join(dir, name), Size: int64(len(content))}
			errs <- fetch(context.Background(), cl, req, 10*time.Second)
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	checkFile(t, filepath.Join(dir, "a"), content)
	checkFile(t, filepath.Join(dir, "b"), content)
}

// The stdio protocol: a dead swarm is reported as a timeout (hapiq stops
// trying that peer's magnets), and a cancel is answered once the fetch has
// let go of dest (hapiq waits for that before removing it).
func TestStdioTimeoutAndCancel(t *testing.T) {
	t.Setenv("HAPIQ_MAGNET_TIMEOUT", "500ms")
	t.Setenv("HAPIQ_MAGNET_LISTEN", "127.0.0.1:0")
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { _ = serveStdio(inR, outW) }()
	t.Cleanup(func() { _ = inW.Close() })
	dec := json.NewDecoder(outR)
	var hello response
	if err := dec.Decode(&hello); err != nil || hello.Protocol != protocolVersion {
		t.Fatalf("hello = %+v, %v", hello, err)
	}

	dir := t.TempDir()
	for id, ih := range map[int]string{1: strings.Repeat("ab", 20), 2: strings.Repeat("cd", 20)} {
		fmt.Fprintf(inW, `{"op":"fetch","id":%d,"magnet":"magnet:?xt=urn:btih:%s&x.pe=127.0.0.1:1","dest":%q,"size":1}`+"\n",
			id, ih, filepath.Join(dir, ih))
	}
	fmt.Fprintln(inW, `{"op":"cancel","id":2}`)

	got := map[int]response{}
	for range 2 {
		var r response
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		got[r.ID] = r
	}
	if r := got[1]; !r.Timeout || r.Error == "" {
		t.Errorf("dead swarm: %+v, want an error flagged as a timeout", r)
	}
	if r := got[2]; r.Timeout || r.Error == "" {
		t.Errorf("cancel: %+v, want an error that is not a timeout", r)
	}
}

// The default listen address is 0.0.0.0; it must never end up in a magnet.
func TestSeedRefusesUnspecifiedAdvertise(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--listen", ":42069"},
		{"--listen", "127.0.0.1:0", "--advertise", "[::]:42069"},
	} {
		done := make(chan error, 1)
		go func() { done <- seed(append([]string{"--cache-dir", t.TempDir()}, args...)) }()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "--advertise") {
				t.Errorf("seed %v: err = %v, want an --advertise error", args, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("seed %v started seeding", args)
		}
	}
}
