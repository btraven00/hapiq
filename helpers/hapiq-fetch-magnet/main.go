// hapiq-fetch-magnet is hapiq's BitTorrent transfer helper. It is a separate
// binary and a separate Go module so the torrent library never links into
// hapiq itself; the two only share the line protocol described in README.md.
//
//	hapiq-fetch-magnet                 serve fetch requests on stdin/stdout
//	hapiq-fetch-magnet seed [flags]    seed a hapiq cache directory
//	hapiq-fetch-magnet tracker [flags] run the tracker peers find each other through
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// protocolVersion is announced on startup; hapiq ignores a helper whose
// version it does not know.
const protocolVersion = 1

func main() {
	var err error
	switch {
	case len(os.Args) > 1 && os.Args[1] == "seed":
		err = seed(os.Args[2:])
	case len(os.Args) > 1 && os.Args[1] == "tracker":
		err = serveTracker(os.Args[2:])
	default:
		err = serveStdio(os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hapiq-fetch-magnet:", err)
		os.Exit(1)
	}
}

// newClient builds the torrent client both modes share. Peers come from a
// magnet's x.pe address, from the trackers the magnet names, and from peer
// exchange with whoever those lead to.
//
// ponytail: no DHT. The public DHT would announce to the world which datasets
// a node holds, and a private one made only of these helpers proved unreliable
// (short-lived fetchers leave dead addresses in it). A tracker next to the
// public cache does the job; revisit DHT if a swarm with no cache is wanted.
//
// advertise ("" for none) is the host:port other nodes dial; an IP host is
// sent to trackers as the announce's "ip", which a tracker on this host uses
// instead of 127.0.0.1.
func newClient(listen, advertise string, seeding bool) (*torrent.Client, error) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableWebseeds = true
	// A fetcher uploads what it has while it downloads, then leaves; staying
	// around to seed is the seed daemon's job.
	cfg.Seed = seeding
	cfg.SetListenAddr(listen)
	// The client binds every enabled family to the one listen host, so an
	// IPv4 host has to switch IPv6 off or the bind fails.
	if host, _, err := net.SplitHostPort(listen); err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
			cfg.DisableIPv6 = true
		}
	}
	if host, _, err := net.SplitHostPort(advertise); err == nil {
		if ip := net.ParseIP(host); ip.To4() != nil {
			cfg.PublicIp4 = ip.To4()
		} else if ip != nil {
			cfg.PublicIp6 = ip
		}
	}
	// Every torrent brings its own storage (fileAt). Without this the client
	// opens a default file storage in the cwd, with a SQLite .torrent.db.
	cfg.DefaultStorage = noStorage{}
	cfg.Slogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return torrent.NewClient(cfg)
}

// noStorage refuses every torrent: none may be added without its own storage.
type noStorage struct{}

func (noStorage) OpenTorrent(context.Context, *metainfo.Info, metainfo.Hash) (storage.TorrentImpl, error) {
	return storage.TorrentImpl{}, errors.New("torrent added without storage")
}

// fileAt is a storage that keeps every torrent's single file at the path,
// relative to baseDir, chosen by relPath; the name the torrent metadata asks
// for is ignored. That is what stops a hostile magnet from choosing where
// bytes land.
func fileAt(baseDir string, relPath func(info *metainfo.Info) string) storage.ClientImplCloser {
	return storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   baseDir,
		FilePathMaker:   func(o storage.FilePathMakerOpts) string { return relPath(o.Info) },
		PieceCompletion: storage.NewMapPieceCompletion(),
		// The path must be exactly what hapiq asked for, no ".part" suffix.
		UsePartFiles: g.Some(false),
		// Cache GC may delete a blob underneath us; a mapped file would SIGBUS.
		DisableMmap: true,
	})
}

// ── stdio protocol ──────────────────────────────────────────────────────────

type request struct {
	Op     string `json:"op"`
	Magnet string `json:"magnet"`
	Dest   string `json:"dest"`
	ID     int    `json:"id"`
	Size   int64  `json:"size"`
}

type response struct {
	Error    string `json:"error,omitempty"`
	ID       int    `json:"id,omitempty"`
	Protocol int    `json:"protocol,omitempty"`
	Done     bool   `json:"done,omitempty"`
	// Timeout marks an error that means the swarm is dead (no metadata, or
	// no progress), as opposed to a bad request or a cancel.
	Timeout bool `json:"timeout,omitempty"`
}

// errTimeout wraps the errors reported with Timeout set.
var errTimeout = errors.New("timeout")

func serveStdio(in io.Reader, out io.Writer) error {
	listen := os.Getenv("HAPIQ_MAGNET_LISTEN")
	if listen == "" {
		listen = "0.0.0.0:0"
	}
	cl, err := newClient(listen, "", false)
	if err != nil {
		return err
	}
	defer cl.Close()

	var outMu sync.Mutex
	reply := func(r response) {
		outMu.Lock()
		defer outMu.Unlock()
		_ = json.NewEncoder(out).Encode(r)
	}
	reply(response{Protocol: protocolVersion})

	timeout := stallTimeout()
	var mu sync.Mutex
	cancels := map[int]context.CancelFunc{}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		switch req.Op {
		case "fetch":
			ctx, cancel := context.WithCancel(context.Background())
			mu.Lock()
			cancels[req.ID] = cancel
			mu.Unlock()
			go func() {
				err := fetch(ctx, cl, req, timeout)
				mu.Lock()
				delete(cancels, req.ID)
				mu.Unlock()
				cancel()
				if err != nil {
					reply(response{ID: req.ID, Error: err.Error(), Timeout: errors.Is(err, errTimeout)})
					return
				}
				reply(response{ID: req.ID, Done: true})
			}()
		case "cancel":
			mu.Lock()
			if cancel := cancels[req.ID]; cancel != nil {
				cancel()
			}
			mu.Unlock()
		}
	}
	// stdin closed: hapiq is gone, so are we.
	return sc.Err()
}

// stallTimeout bounds both the wait for torrent metadata and any stretch with
// no bytes arriving. hapiq always has the origin URL to fall back to, so a
// quiet swarm is not worth waiting for.
func stallTimeout() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("HAPIQ_MAGNET_TIMEOUT")); err == nil && d > 0 {
		return d
	}
	return 30 * time.Second
}

// busy maps an infohash to a one-slot semaphore held by the fetch using it.
var busy sync.Map

func fetch(ctx context.Context, cl *torrent.Client, req request, stall time.Duration) error {
	if req.Dest == "" || !filepath.IsAbs(req.Dest) {
		return errors.New("dest must be an absolute path")
	}
	spec, err := torrent.TorrentSpecFromMagnetUri(req.Magnet)
	if err != nil {
		return fmt.Errorf("bad magnet: %w", err)
	}
	// hapiq keeps only trackers and x.pe on the answering peer's host; never
	// fetch .torrent files or web seeds from URLs in the magnet either way.
	spec.Sources, spec.Webseeds = nil, nil

	// The client holds one Torrent per infohash, bound to the first request's
	// dest, so a second request for the same infohash waits until the first
	// has dropped it. Released last, after the deferred Drop below.
	// ponytail: the second request downloads again, and one tiny entry per
	// infohash stays for the helper's lifetime (one hapiq run); copy the first
	// file if one dataset often holds the same blob twice.
	sem, _ := busy.LoadOrStore(spec.InfoHash, make(chan struct{}, 1))
	select {
	case sem.(chan struct{}) <- struct{}{}:
		defer func() { <-sem.(chan struct{}) }()
	case <-ctx.Done():
		return ctx.Err()
	}

	store := fileAt(filepath.Dir(req.Dest), func(*metainfo.Info) string { return filepath.Base(req.Dest) })
	defer store.Close()
	spec.Storage = store

	t, _, err := cl.AddTorrentSpec(spec)
	if err != nil {
		return err
	}
	// Drop closes the storage, so the file is complete on disk before we reply.
	defer t.Drop()

	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(stall):
		return fmt.Errorf("%w: no metadata after %s", errTimeout, stall)
	}

	// The magnet came from an untrusted peer. hapiq verifies the sha256 after
	// the fact; these checks stop the cheap abuses before any bytes are written.
	info := t.Info()
	if info.IsDir() || len(info.UpvertedFiles()) != 1 {
		return errors.New("torrent is not a single file")
	}
	if info.TotalLength() != req.Size {
		return fmt.Errorf("torrent is %d bytes, expected %d", info.TotalLength(), req.Size)
	}

	t.DownloadAll()

	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	last, lastChange := int64(-1), time.Now()
	sources := map[string]int64{}
	for {
		select {
		case <-t.Complete().On():
			tallySources(t, sources)
			// One line per file: only on request, so `hapiq --quiet` stays quiet.
			if os.Getenv("HAPIQ_MAGNET_VERBOSE") != "" || os.Getenv("HAPIQ_MAGNET_DEBUG") != "" {
				reportSources(os.Stderr, sources, info.Name)
			}
			if os.Getenv("HAPIQ_MAGNET_DEBUG") != "" {
				cl.WriteStatus(os.Stderr)
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			tallySources(t, sources)
			// BytesCompleted counts the dirty (written, unverified) chunks of
			// incomplete pieces too, so it moves per 16 KiB chunk, not per piece.
			if n := t.BytesCompleted(); n != last {
				last, lastChange = n, time.Now()
			} else if time.Since(lastChange) > stall {
				return fmt.Errorf("%w: stalled at %d of %d bytes for %s", errTimeout, n, req.Size, stall)
			}
		}
	}
}

// tallySources records how many bytes each connected peer has supplied so
// far. It is sampled while the download runs because connections are gone by
// the time the torrent reports complete.
func tallySources(t *torrent.Torrent, sources map[string]int64) {
	for _, pc := range t.PeerConns() {
		st := pc.Stats()
		if n := st.BytesReadUsefulData.Int64(); n > sources[pc.RemoteAddr.String()] {
			sources[pc.RemoteAddr.String()] = n
		}
	}
}

// reportSources prints the tally on stderr. It is the only way to see from
// outside that a file really came from a swarm. Sampled, so a little low.
// name comes from untrusted torrent metadata, hence quoted: it cannot forge a
// line or a sources= field.
func reportSources(w io.Writer, sources map[string]int64, name string) {
	parts := make([]string, 0, len(sources))
	for addr, n := range sources {
		parts = append(parts, fmt.Sprintf("%s=%d", addr, n))
	}
	sort.Strings(parts)
	_, _ = fmt.Fprintf(w, "hapiq-fetch-magnet: fetched %q sources=%s\n", name, strings.Join(parts, ","))
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// ── seeding ─────────────────────────────────────────────────────────────────

// seed serves every blob of a hapiq cache over BitTorrent and publishes one
// magnet per blob in <cache-dir>/magnets/<sha256>. `hapiq cache serve` reads
// those files and hands the magnet out with /v1/resolve; that directory is the
// whole interface between the two processes.
func seed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	cacheDir := fs.String("cache-dir", "", "hapiq cache directory (required)")
	listen := fs.String("listen", "0.0.0.0:42069", "BitTorrent listen address")
	advertise := fs.String("advertise", "", "host:port other nodes dial (default: --listen)")
	interval := fs.Duration("interval", 30*time.Second, "how often to rescan for new blobs")
	trackerList := fs.String("tracker", os.Getenv("HAPIQ_MAGNET_TRACKERS"),
		"comma-separated tracker announce URLs (default: $HAPIQ_MAGNET_TRACKERS)")
	_ = fs.Parse(args)
	trackers := splitList(*trackerList)
	if *cacheDir == "" {
		return errors.New("seed: --cache-dir is required")
	}
	if *advertise == "" {
		*advertise = *listen
	}
	// The address goes into every magnet; 0.0.0.0 or [::] is not one anybody
	// can dial.
	if host, _, err := net.SplitHostPort(*advertise); err != nil || host == "" || net.ParseIP(host).IsUnspecified() {
		return fmt.Errorf("seed: other nodes cannot dial %q; pass --advertise HOST:PORT", *advertise)
	}

	blobDir := filepath.Join(*cacheDir, "blobs", "sha256")
	magnetDir := filepath.Join(*cacheDir, "magnets")

	// Magnets carry our address, so ones left by an earlier run are stale.
	_ = os.RemoveAll(magnetDir)
	if err := os.MkdirAll(magnetDir, 0o750); err != nil {
		return err
	}
	if len(trackers) == 0 {
		// Without a tracker we are the only peer a magnet leads to, so it dies
		// with us. With one, other nodes may still seed the blob: leave the
		// magnets so the cache keeps pointing fetchers at the swarm.
		// ponytail: if nobody else seeds, a fetcher waits out its metadata
		// timeout before falling back to HTTP; have the cache server ask the
		// tracker for live peers if that wait shows up in practice.
		defer os.RemoveAll(magnetDir)
	}

	cl, err := newClient(*listen, *advertise, true)
	if err != nil {
		return err
	}
	defer cl.Close()

	// Blobs live at blobs/sha256/<aa>/<sha256>; the torrent is named <sha256>.
	store := fileAt(blobDir, func(info *metainfo.Info) string {
		return filepath.Join(info.Name[:2], info.Name)
	})
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ponytail: blobs are added, never dropped. A blob the cache GC removes
	// keeps its magnet until restart and fetches of it fail over to HTTP; watch
	// for deletions if caches under GC pressure start seeding.
	// seen includes blobs that failed, so a bad blob is reported once.
	seen := map[string]bool{}
	for {
		paths, _ := filepath.Glob(filepath.Join(blobDir, "*", "*"))
		for _, p := range paths {
			name := filepath.Base(p)
			if seen[name] || !isSHA256Hex(name) {
				continue
			}
			seen[name] = true
			magnet, err := seedBlob(ctx, cl, store, p, *advertise, trackers)
			if err != nil {
				fmt.Fprintf(os.Stderr, "seed: skipping %s: %v\n", name, err)
				continue
			}
			if err := writeFileAtomic(filepath.Join(magnetDir, name), magnet+"\n"); err != nil {
				return err
			}
		}

		// HAPIQ_MAGNET_DEBUG dumps the client's view of torrents, peers and
		// DHT on every rescan.
		if os.Getenv("HAPIQ_MAGNET_DEBUG") != "" {
			cl.WriteStatus(os.Stderr)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(*interval):
		}
	}
}

// seedBlob adds one blob to the client and returns its magnet link once the
// client has re-hashed the file and holds every piece.
func seedBlob(ctx context.Context, cl *torrent.Client, store storage.ClientImpl, path, advertise string, trackers []string) (string, error) {
	// ponytail: fixed 1 MiB pieces and the file is hashed twice (here and in
	// VerifyData). Fine up to tens of GB; scale the piece length and persist
	// the metainfo if startup time on a big cache becomes a problem.
	info := metainfo.Info{PieceLength: 1 << 20}
	if err := info.BuildFromFilePath(path); err != nil {
		return "", err
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		return "", err
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	if len(trackers) > 0 {
		// One tier. The trackers end up in the magnet, so a fetcher needs no
		// configuration of its own to find the rest of the swarm.
		mi.AnnounceList = [][]string{trackers}
	}

	spec := torrent.TorrentSpecFromMetaInfo(&mi)
	spec.Storage = store
	t, _, err := cl.AddTorrentSpec(spec)
	if err != nil {
		return "", err
	}
	if err := t.VerifyDataContext(ctx); err != nil {
		t.Drop()
		return "", err
	}
	if t.BytesMissing() != 0 {
		// Never let the client try to "repair" a cache blob by downloading.
		t.Drop()
		return "", errors.New("blob does not match its own hashes")
	}

	m := mi.Magnet(nil, &info)
	m.Params = map[string][]string{"x.pe": {advertise}}
	return m.String(), nil
}

func writeFileAtomic(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ── tracker ─────────────────────────────────────────────────────────────────

// serveTracker runs a minimal HTTP BitTorrent tracker (BEP 3, compact replies
// per BEP 23): the rendezvous point through which nodes holding the same blob
// find each other. Meant to run next to a public cache. It knows which
// addresses hold which infohash and nothing else.
//
// IPv6 peers go out in "peers6" (BEP 7); every client gets both lists.
//
// ponytail: in-memory. State is rebuilt from re-announces within one interval
// after a restart.
func serveTracker(args []string) error {
	fs := flag.NewFlagSet("tracker", flag.ExitOnError)
	listen := fs.String("listen", ":6969", "HTTP listen address (default: all IPv4 and IPv6 addresses)")
	interval := fs.Duration("interval", 2*time.Minute, "re-announce interval handed to clients")
	trustProxy := fs.Bool("trust-proxy", false,
		"take the client address from the last X-Forwarded-For entry (only behind a reverse proxy that sets it)")
	_ = fs.Parse(args)

	tr := &tracker{interval: *interval, trustProxy: *trustProxy, swarms: map[string]map[netip.AddrPort]time.Time{}}
	go func() {
		for now := range time.Tick(*interval) {
			tr.prune(now)
		}
	}()
	mux := http.NewServeMux()
	mux.Handle("GET /announce", tr)
	srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

type tracker struct {
	swarms     map[string]map[netip.AddrPort]time.Time // infohash -> peer -> last announce
	interval   time.Duration
	mu         sync.Mutex
	trustProxy bool
}

// ttl is how long a peer stays listed without announcing: two missed
// announces and some slack.
func (tr *tracker) ttl() time.Duration { return 2*tr.interval + time.Minute }

// prune forgets peers that stopped announcing and swarms left empty, so a
// stream of announces for random infohashes holds memory for one ttl only.
// ponytail: no cap, so a flood still holds rate × ttl entries; cap the swarm
// count if the tracker ever faces the open internet.
func (tr *tracker) prune(now time.Time) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for ih, swarm := range tr.swarms {
		for peer, seen := range swarm {
			if now.Sub(seen) > tr.ttl() {
				delete(swarm, peer)
			}
		}
		if len(swarm) == 0 {
			delete(tr.swarms, ih)
		}
	}
}

// peerAddr is the address an announce is recorded under: where the request
// came from, never a parameter, so a client cannot enrol somebody else in a
// swarm. Two exceptions, both from parties the operator trusts: a reverse
// proxy's X-Forwarded-For with --trust-proxy, and a client on this very host
// (a seeder next to the tracker, which would otherwise be listed as 127.0.0.1)
// naming its address in "ip".
func (tr *tracker) peerAddr(r *http.Request) (netip.Addr, error) {
	src, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, err
	}
	addr := src.Addr().Unmap()
	switch xff := r.Header.Values("X-Forwarded-For"); {
	case tr.trustProxy && len(xff) > 0:
		// The last entry is the one our proxy appended. If it is not an
		// address, the proxy's own (loopback) address must not stand in.
		last := xff[len(xff)-1]
		a, err := netip.ParseAddr(strings.TrimSpace(last[strings.LastIndex(last, ",")+1:]))
		if err != nil {
			return netip.Addr{}, fmt.Errorf("bad X-Forwarded-For %q", last)
		}
		addr = a.Unmap()
	case len(xff) > 0:
		// Relayed by a proxy we were not told to trust: the sender is not
		// necessarily on this host, whatever the source address says.
		return addr, nil
	}
	// ponytail: a same-host proxy that sets no X-Forwarded-For (a plain TCP
	// relay) still makes every client look local; such a setup must run the
	// tracker behind a proxy that sets it, with --trust-proxy.
	if addr.IsLoopback() {
		if a, err := netip.ParseAddr(r.URL.Query().Get("ip")); err == nil {
			addr = a.Unmap()
		}
	}
	return addr, nil
}

func (tr *tracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	infoHash := q.Get("info_hash")
	port, _ := strconv.ParseUint(q.Get("port"), 10, 16)
	addr, err := tr.peerAddr(r)
	if len(infoHash) != 20 || port == 0 || err != nil {
		http.Error(w, "bad announce", http.StatusBadRequest)
		return
	}
	self := netip.AddrPortFrom(addr, uint16(port))

	tr.mu.Lock()
	swarm := tr.swarms[infoHash]
	if swarm == nil {
		swarm = map[netip.AddrPort]time.Time{}
		tr.swarms[infoHash] = swarm
	}
	if q.Get("event") == "stopped" {
		delete(swarm, self)
	} else {
		swarm[self] = time.Now()
	}
	var v4, v6 []byte // compact: 4 or 16 address bytes, then the port
	for peer, seen := range swarm {
		switch {
		case time.Since(seen) > tr.ttl():
			delete(swarm, peer)
		case peer == self:
		case peer.Addr().Is4():
			v4 = append(append(v4, peer.Addr().AsSlice()...), byte(peer.Port()>>8), byte(peer.Port()))
		default:
			v6 = append(append(v6, peer.Addr().AsSlice()...), byte(peer.Port()>>8), byte(peer.Port()))
		}
	}
	if len(swarm) == 0 {
		delete(tr.swarms, infoHash)
	}
	tr.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain")
	_ = bencode.NewEncoder(w).Encode(map[string]any{
		"interval": int64(tr.interval / time.Second),
		"peers":    string(v4),
		"peers6":   string(v6),
	})
}
