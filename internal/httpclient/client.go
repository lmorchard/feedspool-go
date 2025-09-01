package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// DefaultUserAgent mimics a common browser to avoid blocking
	DefaultUserAgent = "Mozilla/5.0 (compatible; feedspool/1.0; +https://github.com/lmorchard/feedspool-go)"
	DefaultTimeout   = 30 * time.Second
	MaxResponseSize  = 100 * 1024 // 100KB for metadata fetching
)

// Client is a shared HTTP client for feedspool.
type Client struct {
	httpClient      *http.Client
	userAgent       string
	timeout         time.Duration
	maxResponseSize int64
}

// Config holds configuration for the HTTP client.
type Config struct {
	Timeout         time.Duration
	UserAgent       string
	MaxResponseSize int64
}

// NewClient creates a new HTTP client with the given configuration.
func NewClient(config *Config) *Client {
	if config == nil {
		config = &Config{}
	}

	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}
	if config.UserAgent == "" {
		config.UserAgent = DefaultUserAgent
	}
	if config.MaxResponseSize == 0 {
		config.MaxResponseSize = MaxResponseSize
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: config.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 10 redirects
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				// Preserve headers on redirect
				if len(via) > 0 {
					req.Header.Set("User-Agent", via[0].Header.Get("User-Agent"))
				}
				return nil
			},
		},
		userAgent:       config.UserAgent,
		timeout:         config.Timeout,
		maxResponseSize: config.MaxResponseSize,
	}
}

// Request represents an HTTP request with additional options.
type Request struct {
	URL               string
	Method            string
	Headers           map[string]string
	Body              io.Reader
	Context           context.Context //nolint:containedctx // Context is needed for request lifecycle
	LimitResponseSize bool
}

// Response wraps the HTTP response with additional metadata.
type Response struct {
	*http.Response
	BodyReader io.Reader
}

// Do performs an HTTP request with the configured client.
func (c *Client) Do(req *Request) (*Response, error) {
	logrus.Debugf("HTTP %s %s (timeout: %v)", req.Method, req.URL, c.timeout)

	if req.Context == nil {
		// Note: We don't use defer cancel() here because the context needs to remain
		// valid while the caller reads the response body. The timeout will still
		// apply to the overall HTTP client operation.
		req.Context, _ = context.WithTimeout(context.Background(), c.timeout)
	}

	if req.Method == "" {
		req.Method = "GET"
	}

	httpReq, err := http.NewRequestWithContext(req.Context, req.Method, req.URL, req.Body)
	if err != nil {
		logrus.Debugf("Failed to create HTTP request for %s: %v", req.URL, err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	httpReq.Header.Set("User-Agent", c.userAgent)

	// Apply custom headers
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(httpReq) //nolint:bodyclose // Response body is closed by caller
	if err != nil {
		logrus.Debugf("HTTP request failed for %s: %v", req.URL, err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	// Note: resp.Body is intentionally not closed here as it's returned to caller

	logrus.Debugf("HTTP %d %s %s (content-length: %d)",
		resp.StatusCode, req.Method, req.URL, resp.ContentLength)

	// Wrap response body with size limiter if requested
	bodyReader := io.Reader(resp.Body)
	if req.LimitResponseSize {
		logrus.Debugf("Applying size limit of %d bytes for %s", c.maxResponseSize, req.URL)
		bodyReader = &limitedReader{
			reader: resp.Body,
			limit:  c.maxResponseSize,
		}
	}

	// Note: Response.Body.Close() should be called by the caller
	return &Response{
		Response:   resp,
		BodyReader: bodyReader,
	}, nil
}

// Get performs a simple GET request.
func (c *Client) Get(url string) (*Response, error) {
	return c.Do(&Request{
		URL:    url,
		Method: "GET",
	})
}

// GetWithHeaders performs a GET request with custom headers.
func (c *Client) GetWithHeaders(url string, headers map[string]string) (*Response, error) {
	return c.Do(&Request{
		URL:     url,
		Method:  "GET",
		Headers: headers,
	})
}

// GetLimited performs a GET request with response size limiting.
func (c *Client) GetLimited(url string) (*Response, error) {
	return c.Do(&Request{
		URL:               url,
		Method:            "GET",
		LimitResponseSize: true,
	})
}

// limitedReader wraps an io.Reader to limit the number of bytes read.
type limitedReader struct {
	reader io.Reader
	limit  int64
	read   int64
}

func (lr *limitedReader) Read(p []byte) (n int, err error) {
	if lr.read >= lr.limit {
		return 0, io.EOF
	}

	remaining := lr.limit - lr.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}

	n, err = lr.reader.Read(p)
	lr.read += int64(n)

	// If we've hit the limit, return EOF on next read
	if lr.read >= lr.limit && err == nil {
		// Read a bit more to check if there's more data
		dummy := make([]byte, 1)
		if _, readErr := lr.reader.Read(dummy); readErr == nil {
			// There's more data, but we're at limit
			return n, nil // Will return EOF on next read
		}
	}

	return n, err
}
