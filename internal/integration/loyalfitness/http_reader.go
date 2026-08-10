package loyalfitness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPReader{baseURL: parsed, token: token, client: client}, nil
}

func (r *HTTPReader) FindMembers(ctx context.Context, actor Actor, filter MemberFilter, page httpx.PageRequest) (MembersPage, error) {
	var err error
	if err := validateActor(actor); err != nil {
		return MembersPage{}, err
	}
	if page, err = normalizePage(page); err != nil {
		return MembersPage{}, err
	}
	if err := validateMemberFilter(filter); err != nil {
		return MembersPage{}, err
	}
	var response httpx.Envelope[[]Member]
	values := actorValues(actor)
	setPageValues(values, page)
	setOptional(values, "q", filter.Query)
	setOptional(values, "status", filter.Status)
	if err := r.get(ctx, "/api/v1/members", values, &response); err != nil {
		return MembersPage{}, err
	}
	if len(response.Data) > page.Limit {
		return MembersPage{}, fmt.Errorf("%w: member page exceeds requested limit", ErrUpstream)
	}
	return MembersPage{Items: response.Data, NextCursor: nextCursor(response.Meta), HasMore: hasMore(response.Meta)}, nil
}

func (r *HTTPReader) FindPTSessions(ctx context.Context, actor Actor, filter PTSessionFilter, page httpx.PageRequest) (PTSessionsPage, error) {
	var err error
	if err := validateActor(actor); err != nil {
		return PTSessionsPage{}, err
	}
	if page, err = normalizePage(page); err != nil {
		return PTSessionsPage{}, err
	}
	if err := validatePTSessionFilter(filter); err != nil {
		return PTSessionsPage{}, err
	}
	var response httpx.Envelope[[]PTSession]
	values := actorValues(actor)
	setPageValues(values, page)
	setOptional(values, "status", filter.Status)
	setOptional(values, "from", filter.From)
	setOptional(values, "to", filter.To)
	if err := r.get(ctx, "/api/v1/pt-sessions", values, &response); err != nil {
		return PTSessionsPage{}, err
	}
	if len(response.Data) > page.Limit {
		return PTSessionsPage{}, fmt.Errorf("%w: PT session page exceeds requested limit", ErrUpstream)
	}
	return PTSessionsPage{Items: response.Data, NextCursor: nextCursor(response.Meta), HasMore: hasMore(response.Meta)}, nil
}

func (r *HTTPReader) FinanceSummary(ctx context.Context, actor Actor) (FinanceSummary, error) {
	if err := validateActor(actor); err != nil {
		return FinanceSummary{}, err
	}
	var response httpx.Envelope[FinanceSummary]
	if err := r.get(ctx, "/api/v1/finance/summary", actorValues(actor), &response); err != nil {
		return FinanceSummary{}, err
	}
	return response.Data, nil
}

func (r *HTTPReader) MobileDashboard(ctx context.Context, actor Actor) (MobileDashboard, error) {
	if err := validateActor(actor); err != nil {
		return MobileDashboard{}, err
	}
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
	const maxResponseBytes = 2 << 20
	if response.ContentLength > maxResponseBytes {
		return fmt.Errorf("%w: upstream response is too large", ErrUpstream)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: read upstream response: %v", ErrUpstream, err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("%w: upstream response is too large", ErrUpstream)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode upstream response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode upstream response: trailing data")
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
