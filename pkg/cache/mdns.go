package cache

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

const (
	// mdnsService is the DNS-SD service type. Browsing it yields PTR records
	// naming each node, each with an SRV record (host + port) and a TXT record.
	mdnsService = "_hapiq._tcp"
	mdnsDomain  = "local."

	// mdnsBrowseTimeout bounds discovery at startup. mDNS answers arrive within
	// milliseconds on a healthy link; anything slower is not worth blocking a
	// download for.
	mdnsBrowseTimeout = 2 * time.Second
)

// AdvertiseMDNS announces this cache as a `_hapiq._tcp` service on the local
// link, so peers on the same network segment find it without configuration.
// The returned func stops the announcement (sending mDNS goodbye records).
//
// mDNS is link-local by design: announcements carry TTL 255 and are dropped by
// routers, so this reaches exactly one broadcast domain and nothing beyond it.
// For anything routed — another subnet, a datacenter, a VPN — use an introducer
// instead.
func (s *Server) AdvertiseMDNS() (stop func(), err error) {
	_, portStr, err := net.SplitHostPort(s.cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("parse listen address: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("parse listen port: %w", err)
	}

	instance, err := os.Hostname()
	if err != nil || instance == "" {
		instance = "hapiq"
	}

	// TXT carries what a browsing client needs to decide whether to bother
	// resolving against us, without a second round-trip.
	txt := []string{"v=1"}
	if n, err := s.cache.BlobCount(context.Background()); err == nil {
		txt = append(txt, "blobs="+strconv.Itoa(n))
	}
	if s.cfg.Token != "" {
		txt = append(txt, "auth=token")
	}

	srv, err := zeroconf.Register(instance, mdnsService, mdnsDomain, port, txt, nil)
	if err != nil {
		return nil, fmt.Errorf("mdns register: %w", err)
	}
	return srv.Shutdown, nil
}

// DiscoverMDNS browses the local link for other hapiq caches and adds them to
// the peer set. Errors are non-fatal: discovery is a convenience, and every
// other bootstrap path still applies.
func (p *Peers) DiscoverMDNS(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, mdnsBrowseTimeout)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry, 16)
	go func() {
		if err := zeroconf.Browse(ctx, mdnsService, mdnsDomain, entries); err != nil {
			fmt.Fprintf(os.Stderr, "cache: mdns browse: %v\n", err)
		}
	}()

	for entry := range entries {
		for _, u := range entryURLs(entry) {
			p.Add(u)
		}
	}
}

// entryURLs turns one mDNS answer into candidate base URLs, preferring the
// literal addresses in the A/AAAA records over the .local hostname (which needs
// a working mDNS resolver on this host to be usable at all).
func entryURLs(entry *zeroconf.ServiceEntry) []string {
	if entry == nil || entry.Port == 0 {
		return nil
	}

	port := strconv.Itoa(entry.Port)
	var out []string
	for _, ip := range entry.AddrIPv4 {
		out = append(out, "http://"+net.JoinHostPort(ip.String(), port))
	}
	for _, ip := range entry.AddrIPv6 {
		if ip.IsLinkLocalUnicast() {
			// A link-local address needs a zone identifier we do not have here.
			continue
		}
		out = append(out, "http://"+net.JoinHostPort(ip.String(), port))
	}
	if len(out) == 0 && entry.HostName != "" {
		out = append(out, "http://"+net.JoinHostPort(strings.TrimSuffix(entry.HostName, "."), port))
	}
	return out
}
