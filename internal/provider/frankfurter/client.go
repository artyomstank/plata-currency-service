package frankfurter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBody = 1 << 20

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	endpoint *url.URL
	http     httpDoer
}

func NewClient(baseURL string, httpClient httpDoer) (*Client, error) {
	endpoint, err := url.Parse(strings.TrimRight(baseURL, "/") + "/latest")
	if err != nil {
		return nil, fmt.Errorf("parse Frankfurter URL: %w", err)
	}
	if (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid Frankfurter URL: HTTP(S) host is required")
	}
	return &Client{endpoint: endpoint, http: httpClient}, nil
}

func (c *Client) Latest(ctx context.Context, input latestRequest) (*latestResponse, error) {
	endpoint := *c.endpoint
	query := endpoint.Query()
	query.Set("from", input.Base)
	query.Set("to", input.Quote)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Frankfurter request: %w", err)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Frankfurter: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Frankfurter returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("read Frankfurter response: %w", err)
	}
	if len(body) > maxResponseBody {
		return nil, fmt.Errorf("Frankfurter response exceeds %d bytes", maxResponseBody)
	}
	var result latestResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode Frankfurter response: %w", err)
	}
	return &result, nil
}
