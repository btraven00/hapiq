package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// peerTTL is how long a peer stays in the registry after its last request.
const peerTTL = 10 * time.Minute

// Server exposes a Cache to other hapiq instances over HTTP. Blobs are keyed by
// sha256, so a client verifies every byte it receives against the key it asked
// for: an untrusted or buggy server cannot corrupt a caller's data, it can only
// fail to be useful.
type Server struct {
	cache *Cache
	cfg   ServerConfig
	peers *peerRegistry
}

// NewServer returns a Server exposing c under cfg.
func NewServer(c *Cache, cfg ServerConfig) *Server {
	return &Server{cache: c, cfg: cfg, peers: newPeerRegistry()}
}

// Handler returns the HTTP handler implementing the v1 API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /v1/blob/{hash}", s.guard(s.handleBlob))
	mux.Handle("HEAD /v1/blob/{hash}", s.guard(s.handleBlob))
	mux.Handle("GET /v1/resolve", s.guard(s.handleResolve))
	mux.Handle("GET /v1/peers", s.guard(s.handlePeers))
	return mux
}

// ListenAndServe serves until ctx is cancelled, then shuts down gracefully.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}

// guard wraps h with bearer-token auth (when configured) and passive peer
// registration: any request carrying a usable X-Hapiq-Self header adds the
// sender to the peer registry. That is the whole "introducer" protocol — a node
// that fetches from us has thereby announced itself, so there is nothing to
// register, subscribe to, or tear down.
func (s *Server) guard(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token != "" && r.Header.Get("Authorization") != "Bearer "+s.cfg.Token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hapiq"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if self := validPeerURL(r.Header.Get("X-Hapiq-Self")); self != "" {
			s.peers.add(self)
		}
		h(w, r)
	})
}

// handleBlob streams a blob by its sha256. http.ServeContent gives Range and
// If-None-Match support for free, so an interrupted transfer resumes.
func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !isSHA256Hex(hash) {
		http.Error(w, "hash must be 64 lowercase hex characters", http.StatusBadRequest)
		return
	}

	// The path is built from validated hex, never from raw request input.
	f, err := os.Open(s.cache.blobPath(hash))
	if err != nil {
		http.Error(w, "no such blob", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "no such blob", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"sha256:`+hash+`"`)
	http.ServeContent(w, r, hash, info.ModTime(), f)
}

// resolveResponse is the body of GET /v1/resolve.
type resolveResponse struct {
	SHA256   string `json:"sha256"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size"`
}

// handleResolve maps an origin URL to the sha256 of its cached content.
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}

	hash, size, hit, err := s.cache.Get(r.Context(), rawURL)
	if err != nil {
		http.Error(w, "lookup failed", http.StatusBadRequest)
		return
	}
	if !hit {
		http.Error(w, "not cached", http.StatusNotFound)
		return
	}

	filename, _ := s.cache.Filename(r.Context(), rawURL)
	writeJSON(w, resolveResponse{SHA256: hash, Size: size, Filename: filename})
}

// peersResponse is the body of GET /v1/peers.
type peersResponse struct {
	Peers []peerEntry `json:"peers"`
}

type peerEntry struct {
	URL      string `json:"url"`
	LastSeen int64  `json:"last_seen"`
}

// handlePeers returns the peers seen recently, plus this node's own advertised
// URL, so a single introducer hostname bootstraps a caller into the whole set.
func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	entries := s.peers.list()
	if self := s.selfURL(); self != "" {
		// Never hand a caller back its own address.
		if caller := validPeerURL(r.Header.Get("X-Hapiq-Self")); caller != self {
			entries = append(entries, peerEntry{URL: self, LastSeen: time.Now().Unix()})
		}
	}
	writeJSON(w, peersResponse{Peers: entries})
}

// AdvertisedURL returns the base URL this node tells peers to use, or "" if it
// could not be determined.
func (s *Server) AdvertisedURL() string { return s.selfURL() }

func (s *Server) selfURL() string { return advertisedURL(s.cfg) }

// advertisedURL returns the base URL a node tells peers to use. An explicit
// cache.server.advertise wins; otherwise it is derived from the listen address,
// substituting the outbound interface IP for a wildcard bind. Returns "" when
// the node does not share its cache, since it then has nothing to announce.
func advertisedURL(cfg ServerConfig) string {
	if cfg.Advertise != "" {
		return cfg.Advertise
	}

	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		if host = outboundIP(); host == "" {
			return ""
		}
	}
	return "http://" + net.JoinHostPort(host, port)
}

// outboundIP reports the local address the kernel would use to reach the wider
// network. The UDP "connection" sends no packets; it only forces a route
// lookup, so it works offline as long as a default route exists.
func outboundIP() string {
	conn, err := net.Dial("udp", "192.0.2.1:9") // TEST-NET-1, never routed
	if err != nil {
		return ""
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return addr.IP.String()
}

// --- peer registry ---

// peerRegistry tracks peers that have identified themselves recently.
//
// ponytail: in-memory only. A restarted introducer repopulates itself the first
// time each peer talks to it; persist to sqlite only if restart churn actually
// proves annoying.
type peerRegistry struct {
	seen map[string]time.Time
	mu   sync.Mutex
}

func newPeerRegistry() *peerRegistry {
	return &peerRegistry{seen: make(map[string]time.Time)}
}

func (p *peerRegistry) add(peerURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen[peerURL] = time.Now()
}

func (p *peerRegistry) list() []peerEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	cutoff := time.Now().Add(-peerTTL)
	out := make([]peerEntry, 0, len(p.seen))
	for u, t := range p.seen {
		if t.Before(cutoff) {
			delete(p.seen, u)
			continue
		}
		out = append(out, peerEntry{URL: u, LastSeen: t.Unix()})
	}
	return out
}

// --- helpers ---

// isSHA256Hex reports whether s is exactly 64 lowercase hex characters. Callers
// use it to vet request input before it reaches the filesystem: a value that
// passes cannot contain a path separator or a dot, so blobPath cannot escape
// the cache directory.
func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validPeerURL returns a normalized base URL if raw is a plausible peer address,
// or "" if it is not. Peers announce themselves in a header, so this is a trust
// boundary: reject anything that is not a bare http(s) origin.
func validPeerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 255 {
		return ""
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return ""
	}

	return u.Scheme + "://" + u.Host
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cache serve: write response: %v\n", err)
	}
}
