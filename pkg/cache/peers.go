package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type peersContextKey struct{}

// DefaultPeerTimeout is the base unit for every peer deadline: the TCP dial and
// each /v1/resolve or /v1/peers round-trip get exactly this, TLS handshake and
// response headers get a multiple of it. A peer that is slow to answer is not
// worth waiting for — the origin URL is always there as a fallback.
//
// Raise cache.server.timeout when peers are reached over a high-latency link
// such as a cross-continent VPN, where the LAN-tuned default is too tight and
// peers would be silently pruned as unreachable.
const DefaultPeerTimeout = 2 * time.Second

// Peers is the client-side view of the peer set: the nodes consulted, in order,
// before falling back to a file's origin URL.
//
// Peer selection deliberately carries no notion of trust or reputation. Blobs
// are addressed by sha256 and the caller verifies the hash while streaming, so
// the worst a hostile peer can do is waste one round-trip. That makes "pick a
// node" a plain loop rather than a distributed-systems problem.
type Peers struct {
	client  *http.Client
	self    string
	token   string
	timeout time.Duration
	urls    []string
	// rr rotates the peer a resolve starts from, so load spreads instead of
	// always landing on the first entry.
	rr atomic.Uint64
	// mu guards urls: downloads run concurrently (--parallel), and Resolve
	// prunes unreachable peers as it goes.
	mu sync.RWMutex
}

// PeerHit describes a successful resolve against one peer.
type PeerHit struct {
	// Base is the peer base URL that answered.
	Base string
	// SHA256 is the expected content hash. The caller MUST verify it.
	SHA256 string
	// Filename is the peer's recorded Content-Disposition filename, if any.
	Filename string
	// Size is the blob size in bytes.
	Size int64
}

// NewPeers builds a peer client from cfg. It does not contact anything; call
// Bootstrap to expand introducers into peers.
func NewPeers(cfg ServerConfig) *Peers {
	// A node announces itself only if it actually shares its cache. Serving
	// nodes that did not pin an explicit advertise URL get one derived from
	// their listen address, so `enabled = true` is the only config an
	// introducer-based setup needs.
	self := cfg.Advertise
	if self == "" && cfg.Enabled {
		self = advertisedURL(cfg)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultPeerTimeout
	}

	p := &Peers{
		token:   cfg.Token,
		self:    self,
		timeout: timeout,
		client: &http.Client{
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
				TLSHandshakeTimeout: 2 * timeout,
				// A peer that accepts the connection and then goes quiet —
				// a laptop that slept or left the wifi — must not stall the
				// download. There is deliberately no overall client timeout:
				// blob transfers are arbitrarily large.
				//
				// ponytail: this catches a stall before the response headers,
				// which is the common case. A peer that starts sending and
				// then trickles is only caught by TCP retransmission (~15
				// min). Add a per-read idle deadline if that shows up in
				// practice.
				ResponseHeaderTimeout: 3 * timeout,
			},
		},
	}
	for _, u := range cfg.Peers {
		p.Add(u)
	}
	return p
}

// Add appends a peer if it is a well-formed base URL, not already present, and
// not this node itself.
func (p *Peers) Add(rawURL string) {
	base := validPeerURL(rawURL)
	if base == "" || base == p.self {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, existing := range p.urls {
		if existing == base {
			return
		}
	}
	p.urls = append(p.urls, base)
}

// ordered returns the peers to try, rotated by one position per call so
// consecutive files spread across the set rather than all hitting the first
// peer. Order within a single resolve is still a strict fallback chain: the
// first peer that answers wins.
//
// ponytail: round-robin, not least-loaded or latency-ranked. It removes the
// hotspot, which is the part that actually hurts; rank peers by measured
// latency only if a real deployment shows it matters.
func (p *Peers) ordered() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	n := len(p.urls)
	if n == 0 {
		return nil
	}

	start := int((p.rr.Add(1) - 1) % uint64(n))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, p.urls[(start+i)%n])
	}
	return out
}

// drop removes a peer that turned out to be unreachable. Discovery is generous
// — mDNS advertises on every interface, so a host with docker bridges offers
// several addresses of which only one works — and without this, every
// unreachable address would cost a dial timeout on every single file.
func (p *Peers) drop(base string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, u := range p.urls {
		if u == base {
			p.urls = append(p.urls[:i], p.urls[i+1:]...)
			return
		}
	}
}

// Len returns the number of known peers.
func (p *Peers) Len() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.urls)
}

// URLs returns the current peer base URLs in preference order.
func (p *Peers) URLs() []string {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]string(nil), p.urls...)
}

// Bootstrap asks each introducer for its peer list and merges the results.
// Failures are silent: an unreachable introducer is not an error, it just means
// fewer peers. Introducers are themselves peers, so they are added too.
func (p *Peers) Bootstrap(ctx context.Context, introducers []string) {
	for _, intro := range introducers {
		p.Add(intro)

		var body peersResponse
		if err := p.getJSON(ctx, intro+"/v1/peers", &body); err != nil {
			continue
		}
		for _, e := range body.Peers {
			p.Add(e.URL)
		}
	}
}

// Resolve asks each peer in turn whether it has rawURL cached, returning the
// first hit. A peer that errors, 404s, or answers nonsense is skipped.
func (p *Peers) Resolve(ctx context.Context, rawURL string) (PeerHit, bool) {
	if p.Len() == 0 {
		return PeerHit{}, false
	}

	for _, base := range p.ordered() {
		endpoint := base + "/v1/resolve?url=" + url.QueryEscape(rawURL)

		var body resolveResponse
		if err := p.getJSON(ctx, endpoint, &body); err != nil {
			// A transport error means the address is dead; an HTTP status
			// means the peer is alive but does not have this file, which is
			// no reason to forget it.
			var statusErr peerStatusError
			if !errors.As(err, &statusErr) {
				p.drop(base)
			}
			continue
		}
		if !isSHA256Hex(body.SHA256) || body.Size < 0 {
			continue
		}

		return PeerHit{
			Base:     base,
			SHA256:   body.SHA256,
			Filename: body.Filename,
			Size:     body.Size,
		}, true
	}

	return PeerHit{}, false
}

// BlobURL returns the URL to stream the blob described by hit.
func (hit PeerHit) BlobURL() string {
	return hit.Base + "/v1/blob/" + hit.SHA256
}

// Client returns the HTTP client to use for peer transfers. It has a short dial
// timeout (a dead peer must not stall a download) but no overall deadline,
// since blob transfers are arbitrarily large.
func (p *Peers) Client() *http.Client {
	return p.client
}

// Headers returns the headers to attach to peer requests: the shared bearer
// token when configured, and this node's own address so the peer can add us to
// its registry. Passive registration means no separate announce protocol.
func (p *Peers) Headers() map[string]string {
	if p == nil {
		return nil
	}
	h := make(map[string]string, 2)
	if p.token != "" {
		h["Authorization"] = "Bearer " + p.token
	}
	if p.self != "" {
		h["X-Hapiq-Self"] = p.self
	}
	return h
}

// getJSON performs a short-deadline GET and decodes a JSON body.
func (p *Peers) getJSON(ctx context.Context, endpoint string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	for k, v := range p.Headers() {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return peerStatusError{code: resp.StatusCode}
	}

	// Cap the body: a peer is untrusted input, and these responses are tiny.
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// peerStatusError distinguishes "peer answered, but not with what we wanted"
// from "peer is unreachable". Only the latter gets the peer dropped.
type peerStatusError struct{ code int }

func (e peerStatusError) Error() string { return fmt.Sprintf("peer returned HTTP %d", e.code) }

// WithPeers returns a child context carrying p.
func WithPeers(ctx context.Context, p *Peers) context.Context {
	return context.WithValue(ctx, peersContextKey{}, p)
}

// PeersFromContext extracts a Peers from ctx; returns nil if none is set.
func PeersFromContext(ctx context.Context) *Peers {
	v, _ := ctx.Value(peersContextKey{}).(*Peers)
	return v
}

// SetupPeers builds a peer client from cfg and bootstraps it from any
// configured introducers, returning nil when no peers could be found. Intended
// to be called once at command startup.
func SetupPeers(ctx context.Context, cfg ServerConfig) *Peers {
	if len(cfg.Peers) == 0 && len(cfg.Introducers) == 0 && cfg.Discover == "" {
		return nil
	}

	p := NewPeers(cfg)
	p.Bootstrap(ctx, cfg.Introducers)
	if cfg.Discover == "mdns" {
		p.DiscoverMDNS(ctx)
	}

	if p.Len() == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "cache: no peers reachable, using origin URLs\n")
		return nil
	}
	return p
}
