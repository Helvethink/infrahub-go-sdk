package infrahub

import (
	"fmt"
	"net/http"
	"time"
)

const (
	defaultBranch       = "main"
	defaultTimeout      = 30 * time.Second
	defaultMaxBodyBytes = int64(16 << 20)
	defaultUserAgent    = "infrahub-go-sdk/dev"
)

// defaultHTTPClient creates the SDK's default HTTP client.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: defaultTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// config contains validated root-client configuration.
type config struct {
	httpClient   *http.Client
	token        string
	branch       string
	userAgent    string
	headers      http.Header
	maxBodyBytes int64
}

// Option configures a Client.
type Option func(*config) error

// WithHTTPClient makes the client use hc. The SDK does not mutate hc.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) error {
		if hc == nil {
			return fmt.Errorf("infrahub: HTTP client must not be nil")
		}
		c.httpClient = hc
		return nil
	}
}

// WithAPIToken configures authentication through X-INFRAHUB-KEY.
func WithAPIToken(token string) Option {
	return func(c *config) error {
		c.token = token
		return nil
	}
}

// WithDefaultBranch sets the branch used when an operation does not specify one.
func WithDefaultBranch(branch string) Option {
	return func(c *config) error {
		if branch == "" {
			return fmt.Errorf("infrahub: default branch must not be empty")
		}
		c.branch = branch
		return nil
	}
}

// WithUserAgent changes the User-Agent sent by the SDK.
func WithUserAgent(userAgent string) Option {
	return func(c *config) error {
		if userAgent == "" {
			return fmt.Errorf("infrahub: user agent must not be empty")
		}
		c.userAgent = userAgent
		return nil
	}
}

// WithHeader adds a header to every request. Authentication headers cannot be
// set through this option; use WithAPIToken instead.
func WithHeader(name, value string) Option {
	return func(c *config) error {
		if name == "" {
			return fmt.Errorf("infrahub: header name must not be empty")
		}
		if http.CanonicalHeaderKey(name) == "X-Infrahub-Key" || http.CanonicalHeaderKey(name) == "Authorization" {
			return fmt.Errorf("infrahub: authentication headers require WithAPIToken")
		}
		c.headers.Add(name, value)
		return nil
	}
}

// WithMaxResponseBytes limits response bodies read by the SDK.
func WithMaxResponseBytes(n int64) Option {
	return func(c *config) error {
		if n <= 0 {
			return fmt.Errorf("infrahub: maximum response size must be positive")
		}
		c.maxBodyBytes = n
		return nil
	}
}
