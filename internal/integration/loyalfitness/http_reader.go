package loyalfitness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Ridzz05/NexusAPI/internal/platform/httpx"
)

var ErrUpstream = errors.New("Loyal Fitness upstream request failed")

// HTTPReader is the explicit adapter contract for a Laravel or other source
// service. The source system owns its data; NexusAPI only translates bounded
// read responses into its stable v1 model.
type HTTPReader struct {
	baseURL *url.URL
	token   string
	client  *http.Client
}

func NewHTTPReader(baseURL, token string, client *http.Client) (*HTTPReader, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("Loyal Fitness base URL must be an http or https URL")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPReader{baseURL: parsed, token: token, client: client}, nil
}

func (r *HTTPReader) FindMembers(ctx context.Context, actor Actor, filter MemberFilter, page httpx.PageRequest) (MembersPage, error) {
	var response httpx.Envelope[[]Member]
	values := actorValues(actor)
	setPageValues(values, page)
	setOptional(values, "q", filter.Query)
	setOptional(values, "status", filter.Status)
	if err := r.get(ctx, "/api/v1/members", values, &response); err != nil {
		return MembersPage{}, err
	}
	return MembersPage{Items: response.Data, NextCursor: nextCursor(response.Meta), HasMore: hasMore(response.Meta)}, nil
}

func (r *HTTPReader) FindPTSessions(ctx context.Context, actor Actor, filter PTSessionFilter, page httpx.PageRequest) (PTSessionsPage, error) {
	var response httpx.Envelope[[]PTSession]
	values := actorValues(actor)
	setPageValues(values, page)
	setOptional(values, "status", filter.Status)
	setOptional(values, "from", filter.From)
	setOptional(values, "to", filter.To)
	if err := r.get(ctx, "/api/v1/pt-sessions", values, &response); err != nil {
		return PTSessionsPage{}, err
	}
	return PTSessionsPage{Items: response.Data, NextCursor: nextCursor(response.Meta), HasMore: hasMore(response.Meta)}, nil
}

func (r *HTTPReader) FinanceSummary(ctx context.Context, actor Actor) (FinanceSummary, error) {
	var response httpx.Envelope[FinanceSummary]
	if err := r.get(ctx, "/api/v1/finance/summary", actorValues(actor), &response); err != nil {
		return FinanceSummary{}, err
	}
	return response.Data, nil
}

func (r *HTTPReader) MobileDashboard(ctx context.Context, actor Actor) (MobileDashboard, error) {
	var response httpx.Envelope[MobileDashboard]
	if err := r.get(ctx, "/api/v1/mobile/dashboard", actorValues(actor), &response); err != nil {
		return MobileDashboard{}, err
	}
	return response.Data, nil
}

func (r *HTTPReader) get(ctx context.Context, path string, values url.Values, target any) error {
	endpoint := *r.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	endpoint.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create upstream request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if r.token != "" {
		request.Header.Set("Authorization", "Bearer "+r.token)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%w: status %d", ErrUpstream, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode upstream response: %w", err)
	}
	return nil
}

func actorValues(actor Actor) url.Values {
	values := url.Values{}
	values.Set("actor_subject", actor.Subject)
	values.Set("actor_roles", strings.Join(actor.Roles, ","))
	if actor.CanViewAllMembers() {
		values.Set("actor_scope", "all")
	} else {
		values.Set("actor_scope", "self")
	}
	return values
}

func setPageValues(values url.Values, page httpx.PageRequest) {
	values.Set("limit", strconv.Itoa(page.Limit))
	setOptional(values, "cursor", page.Cursor)
}

func setOptional(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func nextCursor(meta *httpx.Meta) string {
	if meta == nil {
		return ""
	}
	return meta.NextCursor
}

func hasMore(meta *httpx.Meta) bool {
	return meta != nil && meta.HasMore
}
