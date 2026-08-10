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
		if err != nil || parsed < 1 || parsed > MaxPageSize {
			return PageRequest{}, errors.New("limit must be between 1 and 100")
		}
		limit = parsed
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if cursor != "" {
		if _, err := DecodeCursor(cursor); err != nil {
			return PageRequest{}, errors.New("cursor is invalid")
		}
	}
	return PageRequest{Limit: limit, Cursor: cursor}, nil
}

func EncodeCursor(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func DecodeCursor(cursor string) (string, error) {
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
