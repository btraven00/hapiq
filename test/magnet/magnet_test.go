//go:build magnet

// Package magnet is the end-to-end suite for magnet transfers. It builds the
// real hapiq-fetch-magnet helper, runs it as real processes (a seeder and the
// stdio fetcher hapiq spawns), and moves bytes over real BitTorrent on
// loopback. Nothing leaves the machine: the helper has DHT off, the suite
// clears the helper's HAPIQ_MAGNET_* variables, and the only tracker is one
// the suite starts on loopback.
//
// Run with `make magnet-e2e`. Not part of `make test`, because building the
// helper pulls the torrent library.
package magnet

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// helperDir holds the freshly built helper binary.
var helperDir string

func TestMain(m *testing.M) {
	// Hermetic: a tracker or timeout from the developer's shell must not reach
	// the helpers this suite starts.
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "HAPIQ_MAGNET_") {
			_ = os.Unsetenv(name)
		}
	}
	dir, err := os.MkdirTemp("", "hapiq-magnet-helper")
	if err != nil {
		panic(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, cache.MagnetHelper), ".")
	build.Dir = filepath.Join("..", "..", "helpers", "hapiq-fetch-magnet")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building helper:", err)
		os.Exit(1)
	}
	helperDir = dir

	code := m.Run()
	_ = os.RemoveAll(dir)
	// The helpers run in this cwd; the torrent library's default storage
	// would leave a .torrent.db (or .torrent.bolt.db) here.
	if left, _ := filepath.Glob(".torrent*"); len(left) > 0 && code == 0 {
		fmt.Fprintln(os.Stderr, "magnet helpers left", left, "in the cwd")
		code = 1
	}
	os.Exit(code)
}

// world is one origin server plus node A: a populated cache, its HTTP cache
// server, and a seeder process publishing magnets for it.
type world struct {
	t          *testing.T
	origin     *httptest.Server
	server     *httptest.Server // node A's `hapiq cache serve`
	cacheA     *cache.Cache
	seeder     *exec.Cmd
	seederDone chan struct{}     // closed when the seeder process has exited
	files      map[string][]byte // origin path -> content
	originHits atomic.Int64
	blobHits   atomic.Int64 // GET /v1/blob on node A
}

// odd size: several 1 MiB pieces and a short last one.
const fileSize = 3<<20 + 123

func newWorld(t *testing.T, names ...string) *world {
	t.Helper()
	return newWorldSeeding(t, nil, names...)
}

// newWorldSeeding is newWorld with extra arguments for the seeder.
func newWorldSeeding(t *testing.T, seedArgs []string, names ...string) *world {
	t.Helper()
	w := &world{t: t, files: map[string][]byte{}}
	for _, name := range names {
		b := make([]byte, fileSize)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		w.files["/"+name] = b
	}

	w.origin = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, ok := w.files[r.URL.Path]
		if !ok {
			http.NotFound(rw, r)
			return
		}
		w.originHits.Add(1)
		_, _ = rw.Write(b)
	}))
	t.Cleanup(w.origin.Close)

	// Node A downloads every file from the origin, filling its cache.
	w.cacheA = openCache(t)
	ctxA := cache.WithCache(context.Background(), w.cacheA)
	for path := range w.files {
		if _, err := common.Fetch(ctxA, w.origin.URL+path, filepath.Join(t.TempDir(), "f"), common.FetchOptions{}); err != nil {
			t.Fatalf("node A fetch %s: %v", path, err)
		}
	}

	inner := cache.NewServer(w.cacheA, cache.ServerConfig{}).Handler()
	w.server = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/blob/") {
			w.blobHits.Add(1)
		}
		inner.ServeHTTP(rw, r)
	}))
	t.Cleanup(w.server.Close)

	w.seeder = exec.Command(filepath.Join(helperDir, cache.MagnetHelper), append([]string{"seed",
		"--cache-dir", w.cacheA.Dir(), "--listen", freeAddr(t), "--interval", "200ms"}, seedArgs...)...)
	w.seeder.Stderr = os.Stderr
	if err := w.seeder.Start(); err != nil {
		t.Fatalf("start seeder: %v", err)
	}
	w.seederDone = make(chan struct{})
	go func() { _ = w.seeder.Wait(); close(w.seederDone) }()
	t.Cleanup(func() { w.stopSeeder(syscall.SIGTERM) })

	// The seeder publishes a magnet only once it has verified the blob.
	for path := range w.files {
		w.waitMagnet(path)
	}
	return w
}

func (w *world) sha(path string) string {
	sum := sha256.Sum256(w.files[path])
	return hex.EncodeToString(sum[:])
}

func (w *world) waitMagnet(path string) {
	w.t.Helper()
	deadline := time.After(60 * time.Second)
	for w.cacheA.Magnet(w.sha(path)) == "" {
		select {
		case <-w.seederDone:
			w.t.Fatalf("seeder exited before publishing a magnet for %s", path)
		case <-deadline:
			w.t.Fatalf("seeder never published a magnet for %s", path)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// stopSeeder signals the seeder and waits for it to be gone.
func (w *world) stopSeeder(sig os.Signal) {
	_ = w.seeder.Process.Signal(sig)
	<-w.seederDone
}

// fetchAsB downloads path as a fresh node B whose only peer is node A, and
// checks the bytes are right however they arrived.
func (w *world) fetchAsB(path string, withCache bool) common.FetchResult {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	w.t.Cleanup(cancel)

	if withCache {
		ctx = cache.WithCache(ctx, openCache(w.t))
	}
	peers := cache.NewPeers(cache.ServerConfig{Peers: []string{w.server.URL}})
	w.t.Cleanup(peers.Close)
	ctx = cache.WithPeers(ctx, peers)

	dest := filepath.Join(w.t.TempDir(), "out.bin")
	res, err := common.Fetch(ctx, w.origin.URL+path, dest, common.FetchOptions{})
	if err != nil {
		w.t.Fatalf("node B fetch: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		w.t.Fatal(err)
	}
	if !bytes.Equal(got, w.files[path]) {
		w.t.Fatalf("node B got %d bytes that differ from the origin's %d", len(got), len(w.files[path]))
	}
	if res.SHA256 != w.sha(path) {
		w.t.Errorf("result sha256 = %s, want %s", res.SHA256, w.sha(path))
	}
	return res
}

// expectHits asserts how many times node B's download touched the origin and
// node A's HTTP blob endpoint. Node A itself hit the origin once per file.
func (w *world) expectHits(originAfterA, blob int64) {
	w.t.Helper()
	if got := w.originHits.Load() - int64(len(w.files)); got != originAfterA {
		w.t.Errorf("node B hit the origin %d times, want %d", got, originAfterA)
	}
	if got := w.blobHits.Load(); got != blob {
		w.t.Errorf("node B hit /v1/blob %d times, want %d", got, blob)
	}
}

func openCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.Open(cache.Config{Dir: t.TempDir(), LinkStrategy: cache.StrategyHardlink})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// freeAddr returns a loopback address whose port was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func helperOnPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", helperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The point of the whole feature: the bytes travel over BitTorrent, so neither
// the origin nor node A's HTTP blob endpoint is touched.
func TestFetchOverMagnet(t *testing.T) {
	for _, withCache := range []bool{true, false} {
		t.Run(fmt.Sprintf("localCache=%v", withCache), func(t *testing.T) {
			helperOnPath(t)
			w := newWorld(t, "x.bin")

			res := w.fetchAsB("/x.bin", withCache)

			w.expectHits(0, 0)
			if !res.Hit || res.PeerURL != w.server.URL {
				t.Errorf("result = %+v, want a peer hit from %s", res, w.server.URL)
			}
		})
	}
}

// A dataset is many files: fetches share one helper process and may run
// concurrently (--parallel).
func TestParallelFetches(t *testing.T) {
	helperOnPath(t)
	w := newWorld(t, "a.bin", "b.bin", "c.bin")

	ctx := cache.WithCache(context.Background(), openCache(t))
	peers := cache.NewPeers(cache.ServerConfig{Peers: []string{w.server.URL}})
	t.Cleanup(peers.Close)
	ctx = cache.WithPeers(ctx, peers)

	errs := make(chan error, len(w.files))
	for path, want := range w.files {
		go func() {
			dest := filepath.Join(t.TempDir(), "out.bin")
			if _, err := common.Fetch(ctx, w.origin.URL+path, dest, common.FetchOptions{}); err != nil {
				errs <- err
				return
			}
			got, _ := os.ReadFile(dest)
			if !bytes.Equal(got, want) {
				errs <- fmt.Errorf("%s: wrong bytes", path)
				return
			}
			errs <- nil
		}()
	}
	for range w.files {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	w.expectHits(0, 0)
}

// Magnets are optional: a node without the helper installed must behave
// exactly as before and take the HTTP blob.
func TestNoHelperFallsBackToHTTP(t *testing.T) {
	w := newWorld(t, "x.bin")
	t.Setenv("PATH", t.TempDir()) // no helper anywhere

	w.fetchAsB("/x.bin", true)

	w.expectHits(0, 1)
}

// A magnet whose swarm is gone must cost a bounded wait, then fall back.
func TestDeadSwarmFallsBackToHTTP(t *testing.T) {
	helperOnPath(t)
	t.Setenv("HAPIQ_MAGNET_TIMEOUT", "2s")
	w := newWorld(t, "x.bin")

	// SIGKILL: the seeder gets no chance to withdraw its magnets.
	w.stopSeeder(syscall.SIGKILL)
	if w.cacheA.Magnet(w.sha("/x.bin")) == "" {
		t.Fatal("test setup: magnet should outlive a killed seeder")
	}

	start := time.Now()
	w.fetchAsB("/x.bin", true)

	w.expectHits(0, 1)
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("fallback took %s, want it bounded by the 2s stall timeout", d)
	}
}

// The trust boundary: a peer that hands out a magnet for different bytes of
// the right size must not get them into node B's output or cache.
func TestMagnetForWrongContentIsRejected(t *testing.T) {
	helperOnPath(t)
	w := newWorld(t, "x.bin", "y.bin")

	// Node A now lies: x's magnet points at y's torrent.
	magnets := filepath.Join(w.cacheA.Dir(), "magnets")
	lie, err := os.ReadFile(filepath.Join(magnets, w.sha("/y.bin")))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(magnets, w.sha("/x.bin")), lie, 0o640); err != nil {
		t.Fatal(err)
	}

	// fetchAsB checks node B ended up with x's real bytes; they can only have
	// come from the HTTP blob after the swarm's copy failed verification.
	w.fetchAsB("/x.bin", true)

	w.expectHits(0, 1)
}

// With a tracker, other nodes may still seed the blob, so a clean shutdown
// leaves the magnets and the cache keeps pointing fetchers at the swarm.
func TestSeederWithTrackerKeepsMagnetsOnExit(t *testing.T) {
	addr := freeAddr(t)
	tracker := exec.Command(filepath.Join(helperDir, cache.MagnetHelper), "tracker", "--listen", addr)
	tracker.Stderr = os.Stderr
	if err := tracker.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tracker.Process.Kill(); _ = tracker.Wait() })

	w := newWorldSeeding(t, []string{"--tracker", "http://" + addr + "/announce"}, "x.bin")
	w.stopSeeder(syscall.SIGTERM)

	m := w.cacheA.Magnet(w.sha("/x.bin"))
	if !strings.Contains(m, "tr=") {
		t.Errorf("magnet after a clean stop = %q, want it kept, naming the tracker", m)
	}
}

// A clean shutdown withdraws the magnets, so clients stop being pointed at a
// seeder that is no longer there.
func TestSeederWithdrawsMagnetsOnExit(t *testing.T) {
	w := newWorld(t, "x.bin")

	w.stopSeeder(syscall.SIGTERM)

	if m := w.cacheA.Magnet(w.sha("/x.bin")); m != "" {
		t.Errorf("magnet still published after seeder exit: %s", m)
	}
}
