package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

const (
	defaultUserAgent   = "webhook-delivery-service"
	maxResponseBodyLen = 64 << 10
)

type Options struct {
	Timeout   time.Duration
	UserAgent string
}

type Client struct {
	httpClient *http.Client
	userAgent  string
}

func New(opts Options) (*Client, error) {
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("http timeout = %s, must be greater than 0", opts.Timeout)
	}

	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	httpClient := &http.Client{
		Timeout: opts.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Client{
		httpClient: httpClient,
		userAgent:  userAgent,
	}, nil
}

func (c *Client) Send(ctx context.Context, job domain.DeliveryJob) (int, error) {
	targetURL, err := validateTargetURL(job.TargetURL)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		targetURL.String(),
		bytes.NewReader(job.Payload),
	)
	if err != nil {
		return 0, fmt.Errorf("%w: create request: %w", domain.ErrPermanentDelivery, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBodyLen))

	return resp.StatusCode, nil
}

func validateTargetURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: parse target url: %w", domain.ErrPermanentDelivery, err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf(
			"%w: unsupported target scheme %q",
			domain.ErrPermanentDelivery,
			parsed.Scheme,
		)
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("%w: target url has no host", domain.ErrPermanentDelivery)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("%w: target url must not contain credentials", domain.ErrPermanentDelivery)
	}

	return parsed, nil
}
