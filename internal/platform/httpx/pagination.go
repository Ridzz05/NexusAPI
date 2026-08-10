package httpx

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
	MaxCursorLength = 512
)

type PageRequest struct {
	Limit  int
	Cursor string
}

type Meta struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

func ParsePageRequest(r *http.Request) (PageRequest, error) {
	limit := DefaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return PageRequest{}, errors.New("limit must be between 1 and 100")
		}
		limit = parsed
	}
	page, err := NormalizePageRequest(PageRequest{
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		return PageRequest{}, err
	}
	return page, nil
}

// NormalizePageRequest validates a page at a module boundary. HTTP handlers
// use ParsePageRequest, but adapters and repositories can also be called
// directly and must not accidentally receive an unbounded limit or cursor.
func NormalizePageRequest(page PageRequest) (PageRequest, error) {
	if page.Limit == 0 {
		page.Limit = DefaultPageSize
	}
	if page.Limit < 1 || page.Limit > MaxPageSize {
		return PageRequest{}, errors.New("limit must be between 1 and 100")
	}
	page.Cursor = strings.TrimSpace(page.Cursor)
	if len(page.Cursor) > MaxCursorLength {
		return PageRequest{}, errors.New("cursor is too long")
	}
	if page.Cursor != "" {
		if _, err := DecodeCursor(page.Cursor); err != nil {
			return PageRequest{}, errors.New("cursor is invalid")
		}
	}
	return page, nil
}

func EncodeCursor(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func DecodeCursor(cursor string) (string, error) {
	if len(cursor) > MaxCursorLength {
		return "", errors.New("cursor is too long")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || string(decoded) == "" {
		return "", errors.New("invalid cursor")
	}
	return string(decoded), nil
}

func BoundedQueryParam(r *http.Request, name string, maxLength int) (string, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if len(value) > maxLength {
		return "", errors.New(name + " is too long")
	}
	return value, nil
}
