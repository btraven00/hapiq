// Package ensembl provides protocol abstraction for handling both HTTP and FTP requests.
package ensembl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"
)

// ProtocolClient defines the interface for protocol-agnostic operations.
type ProtocolClient interface {
	// Head performs a HEAD-like operation to check if a resource exists.
	Head(ctx context.Context, urlStr string) (*ProtocolResponse, error)

	// Get retrieves the content of a resource.
	Get(ctx context.Context, urlStr string) (*ProtocolResponse, error)

	// Close cleans up any resources (connections, etc.).
	Close() error
}

// ProtocolResponse represents a unified response from HTTP or FTP operations.
type ProtocolResponse struct {
	StatusCode int
	Header     map[string][]string
	Body       io.ReadCloser
	Size       int64
}

// MultiProtocolClient handles both HTTP and FTP protocols transparently.
type MultiProtocolClient struct {
	httpClient *http.Client
	ftpTimeout time.Duration
	verbose    bool
	ftpPool    *ftpConnectionPool
}

// NewMultiProtocolClient creates a new multi-protocol client.
func NewMultiProtocolClient(httpClient *http.Client, ftpTimeout time.Duration, verbose bool, maxFTPConns ...int) *MultiProtocolClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	maxConns := 2 // Default
	if len(maxFTPConns) > 0 && maxFTPConns[0] > 0 {
		maxConns = maxFTPConns[0]
	}

	return &MultiProtocolClient{
		httpClient: httpClient,
		ftpTimeout: ftpTimeout,
		verbose:    verbose,
		ftpPool:    newFTPConnectionPool(maxConns, ftpTimeout, verbose),
	}
}

// Head performs a HEAD-like operation on the given URL.
func (c *MultiProtocolClient) Head(ctx context.Context, urlStr string) (*ProtocolResponse, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	switch strings.ToLower(parsedURL.Scheme) {
	case "http", "https":
		return c.httpHead(ctx, urlStr)
	case "ftp":
		return c.ftpHead(ctx, parsedURL)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", parsedURL.Scheme)
	}
}

// Get retrieves the content from the given URL.
func (c *MultiProtocolClient) Get(ctx context.Context, urlStr string) (*ProtocolResponse, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	switch strings.ToLower(parsedURL.Scheme) {
	case "http", "https":
		return c.httpGet(ctx, urlStr)
	case "ftp":
		return c.ftpGet(ctx, parsedURL)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", parsedURL.Scheme)
	}
}

// Close cleans up any resources.
func (c *MultiProtocolClient) Close() error {
	// Close FTP connection pool
	if c.ftpPool != nil {
		c.ftpPool.Close()
	}
	return nil
}

// httpHead performs an HTTP HEAD request.
func (c *MultiProtocolClient) httpHead(ctx context.Context, urlStr string) (*ProtocolResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", urlStr, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Convert http.Header to map[string][]string
	header := make(map[string][]string)
	for k, v := range resp.Header {
		header[k] = v
	}

	return &ProtocolResponse{
		StatusCode: resp.StatusCode,
		Header:     header,
		Body:       nil, // HEAD responses don't have bodies
		Size:       resp.ContentLength,
	}, nil
}

// httpGet performs an HTTP GET request.
func (c *MultiProtocolClient) httpGet(ctx context.Context, urlStr string) (*ProtocolResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	// Convert http.Header to map[string][]string
	header := make(map[string][]string)
	for k, v := range resp.Header {
		header[k] = v
	}

	return &ProtocolResponse{
		StatusCode: resp.StatusCode,
		Header:     header,
		Body:       resp.Body,
		Size:       resp.ContentLength,
	}, nil
}

// ftpHead checks if an FTP resource exists (equivalent to HEAD).
func (c *MultiProtocolClient) ftpHead(ctx context.Context, parsedURL *url.URL) (*ProtocolResponse, error) {
	conn, err := c.ftpPool.getConnection(ctx, parsedURL)
	if err != nil {
		return nil, err
	}
	defer c.ftpPool.releaseConnection(conn)

	path := parsedURL.Path
	if path == "" {
		path = "/"
	}

	// Check if the path exists by trying to get its size
	size, err := conn.FileSize(path)
	if err != nil {
		// File doesn't exist or is a directory
		// Try to list the parent directory to see if the file exists
		entries, listErr := conn.List(path)
		if listErr != nil {
			// Path doesn't exist
			return nil, fmt.Errorf("FTP path not found: %s", path)
		}

		// If List returned at least one entry, the path exists as a directory.
		// An empty listing means the path does not exist on this server.
		if len(entries) > 0 {
			return &ProtocolResponse{
				StatusCode: 200,
				Header:     make(map[string][]string),
				Body:       nil,
				Size:       -1, // Directory size is not meaningful
			}, nil
		}
		return nil, fmt.Errorf("FTP path not found (empty listing): %s", path)
	}

	// File exists and we got its size
	return &ProtocolResponse{
		StatusCode: 200,
		Header:     make(map[string][]string),
		Body:       nil,
		Size:       int64(size),
	}, nil
}

// ftpGet retrieves content from an FTP server.
func (c *MultiProtocolClient) ftpGet(ctx context.Context, parsedURL *url.URL) (*ProtocolResponse, error) {
	conn, err := c.ftpPool.getConnection(ctx, parsedURL)
	if err != nil {
		return nil, err
	}

	path := parsedURL.Path
	if path == "" {
		path = "/"
	}

	// Get file size first
	size, err := conn.FileSize(path)
	if err != nil {
		c.ftpPool.releaseConnection(conn)
		return nil, fmt.Errorf("FTP file not found: %s", path)
	}

	// Retrieve the file
	reader, err := conn.Retr(path)
	if err != nil {
		c.ftpPool.releaseConnection(conn)
		return nil, fmt.Errorf("failed to retrieve FTP file: %w", err)
	}

	// Wrap the reader to ensure connection cleanup via pool
	wrappedReader := &ftpReaderCloser{
		reader: reader,
		conn:   conn,
		pool:   c.ftpPool,
	}

	return &ProtocolResponse{
		StatusCode: 200,
		Header:     make(map[string][]string),
		Body:       wrappedReader,
		Size:       int64(size),
	}, nil
}

// connectFTP establishes an FTP connection.
func (c *MultiProtocolClient) connectFTP(ctx context.Context, parsedURL *url.URL) (*ftp.ServerConn, error) {
	host := parsedURL.Host
	if !strings.Contains(host, ":") {
		host += ":21" // Default FTP port
	}

	if c.verbose {
		fmt.Printf("Connecting to FTP server: %s\n", host)
	}

	// Create connection with timeout
	conn, err := ftp.Dial(host, ftp.DialWithTimeout(c.ftpTimeout))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to FTP server: %w", err)
	}

	// Handle authentication
	username := "anonymous"
	password := "anonymous@"

	if parsedURL.User != nil {
		username = parsedURL.User.Username()
		if pass, ok := parsedURL.User.Password(); ok {
			password = pass
		}
	}

	if c.verbose {
		fmt.Printf("Logging in as: %s\n", username)
	}

	err = conn.Login(username, password)
	if err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("failed to login to FTP server: %w", err)
	}

	return conn, nil
}

// ftpReaderCloser wraps an FTP response reader to ensure proper connection cleanup.
type ftpReaderCloser struct {
	reader io.ReadCloser
	conn   *ftp.ServerConn
	pool   *ftpConnectionPool
}

// Read implements io.Reader.
func (r *ftpReaderCloser) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}

// Close implements io.Closer, ensuring both reader and connection are returned to pool.
func (r *ftpReaderCloser) Close() error {
	// Close the reader first
	if r.reader != nil {
		_ = r.reader.Close()
	}

	// Return connection to pool instead of closing it
	if r.pool != nil && r.conn != nil {
		r.pool.releaseConnection(r.conn)
	}

	return nil
}

// ftpConnectionPool manages a pool of FTP connections.
type ftpConnectionPool struct {
	connections chan *ftp.ServerConn
	maxSize     int
	timeout     time.Duration
	verbose     bool
	mu          sync.Mutex
	activeConns map[*ftp.ServerConn]bool
}

// newFTPConnectionPool creates a new FTP connection pool.
func newFTPConnectionPool(maxSize int, timeout time.Duration, verbose bool) *ftpConnectionPool {
	return &ftpConnectionPool{
		connections: make(chan *ftp.ServerConn, maxSize),
		maxSize:     maxSize,
		timeout:     timeout,
		verbose:     verbose,
		activeConns: make(map[*ftp.ServerConn]bool),
	}
}

// getConnection gets an FTP connection from the pool or creates a new one.
func (p *ftpConnectionPool) getConnection(ctx context.Context, parsedURL *url.URL) (*ftp.ServerConn, error) {
	// Try to get an existing connection first
	select {
	case conn := <-p.connections:
		p.mu.Lock()
		p.activeConns[conn] = true
		p.mu.Unlock()

		// Test if connection is still alive
		if err := p.testConnection(conn); err == nil {
			return conn, nil
		}
		// Connection is dead, close it and create a new one
		_ = conn.Quit()
	default:
		// No available connections
	}

	// Create a new connection
	conn, err := p.createConnection(parsedURL)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.activeConns[conn] = true
	p.mu.Unlock()

	return conn, nil
}

// releaseConnection returns a connection to the pool.
func (p *ftpConnectionPool) releaseConnection(conn *ftp.ServerConn) {
	if conn == nil {
		return
	}

	p.mu.Lock()
	delete(p.activeConns, conn)
	p.mu.Unlock()

	// Try to return to pool, if pool is full, close the connection
	select {
	case p.connections <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, close the connection
		_ = conn.Quit()
	}
}

// testConnection tests if an FTP connection is still alive.
func (p *ftpConnectionPool) testConnection(conn *ftp.ServerConn) error {
	// Try a simple NOOP command to test if connection is alive
	return conn.NoOp()
}

// createConnection creates a new FTP connection.
func (p *ftpConnectionPool) createConnection(parsedURL *url.URL) (*ftp.ServerConn, error) {
	host := parsedURL.Host
	if !strings.Contains(host, ":") {
		host += ":21" // Default FTP port
	}

	if p.verbose {
		fmt.Printf("Creating new FTP connection to: %s\n", host)
	}

	// Create connection with timeout
	conn, err := ftp.Dial(host, ftp.DialWithTimeout(p.timeout))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to FTP server: %w", err)
	}

	// Handle authentication
	username := "anonymous"
	password := "anonymous@"

	if parsedURL.User != nil {
		username = parsedURL.User.Username()
		if pass, ok := parsedURL.User.Password(); ok {
			password = pass
		}
	}

	if p.verbose {
		fmt.Printf("Logging in as: %s\n", username)
	}

	err = conn.Login(username, password)
	if err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("failed to login to FTP server: %w", err)
	}

	return conn, nil
}

// Close closes all connections in the pool.
func (p *ftpConnectionPool) Close() {
	// Close all pooled connections
	close(p.connections)
	for conn := range p.connections {
		_ = conn.Quit()
	}

	// Close all active connections
	p.mu.Lock()
	for conn := range p.activeConns {
		_ = conn.Quit()
	}
	p.activeConns = make(map[*ftp.ServerConn]bool)
	p.mu.Unlock()
}
