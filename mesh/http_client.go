package mesh

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const (
	// DefaultFetchTimeout is the default HTTP request timeout for map fetches.
	DefaultFetchTimeout = 30 * time.Second

	// DefaultMaxRetries is the default number of retry attempts.
	DefaultMaxRetries = 3

	// defaultBaseBackoff is the base delay for exponential backoff.
	defaultBaseBackoff = 500 * time.Millisecond

	// maxResponseBytes limits the response body to 50 MB to prevent OOM.
	maxResponseBytes = 50 << 20
)

// FetchOption configures FetchMapFromAPI behavior.
type FetchOption func(*fetchConfig)

type fetchConfig struct {
	timeout     time.Duration
	maxRetries  int
	baseBackoff time.Duration
	client      *http.Client
}

func defaultFetchConfig() fetchConfig {
	return fetchConfig{
		timeout:     DefaultFetchTimeout,
		maxRetries:  DefaultMaxRetries,
		baseBackoff: defaultBaseBackoff,
	}
}

// WithTimeout sets the HTTP request timeout.
func WithTimeout(d time.Duration) FetchOption {
	return func(c *fetchConfig) {
		c.timeout = d
	}
}

// WithMaxRetries sets the maximum number of retry attempts.
func WithMaxRetries(n int) FetchOption {
	return func(c *fetchConfig) {
		c.maxRetries = n
	}
}

// WithBaseBackoff sets the base delay for exponential backoff between retries.
func WithBaseBackoff(d time.Duration) FetchOption {
	return func(c *fetchConfig) {
		c.baseBackoff = d
	}
}

// WithHTTPClient overrides the default HTTP client (useful for testing).
func WithHTTPClient(client *http.Client) FetchOption {
	return func(c *fetchConfig) {
		c.client = client
	}
}

// FetchMapFromAPI fetches a Valetudo map from the given API URL and returns
// the parsed ValetudoMap. It retries transient failures with exponential backoff.
//
// The apiURL should be a full URL, e.g. "https://robot.local/api/v2/robot/state/map".
func FetchMapFromAPI(apiURL string, opts ...FetchOption) (*ValetudoMap, error) {
	return FetchMapFromAPIWithContext(context.Background(), apiURL, opts...)
}

// FetchMapFromAPIWithContext is like FetchMapFromAPI but accepts a context for cancellation.
func FetchMapFromAPIWithContext(ctx context.Context, apiURL string, opts ...FetchOption) (*ValetudoMap, error) {
	if apiURL == "" {
		return nil, fmt.Errorf("fetch map: API URL is empty")
	}

	cfg := defaultFetchConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	client := cfg.client
	if client == nil {
		client = &http.Client{Timeout: cfg.timeout}
	}

	var lastErr error
	for attempt := range cfg.maxRetries {
		if attempt > 0 {
			backoff := cfg.baseBackoff * time.Duration(math.Pow(2, float64(attempt-1)))
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("fetch map: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		body, err := doFetch(ctx, client, apiURL)
		if err != nil {
			lastErr = err
			continue
		}

		m, err := ParseMapJSON(body)
		if err != nil {
			// Parse errors are not transient; do not retry.
			return nil, fmt.Errorf("fetch map: %w", err)
		}
		return m, nil
	}

	return nil, fmt.Errorf("fetch map: all %d attempts failed: %w", cfg.maxRetries, lastErr)
}

// doFetch performs a single HTTP GET and returns the response body bytes.
func doFetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP GET %s: status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}

	return body, nil
}
