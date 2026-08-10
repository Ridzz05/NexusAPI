package loyalfitness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Ridzz05/NexusAPI/internal/platform/cache"
	"github.com/Ridzz05/NexusAPI/internal/platform/httpx"
)

type CachedReader struct {
	next  Reader
	store cache.Store
	ttl   time.Duration
}

func NewCachedReader(next Reader, store cache.Store, ttl time.Duration) *CachedReader {
	return &CachedReader{next: next, store: store, ttl: ttl}
}

func (r *CachedReader) FindMembers(ctx context.Context, actor Actor, filter MemberFilter, page httpx.PageRequest) (MembersPage, error) {
	key := cacheKey("members", actor, filter.Query+"\x00"+filter.Status, page)
	var result MembersPage
	if found, _ := cache.GetJSON(ctx, r.store, key, &result); found {
		return result, nil
	}
	result, err := r.next.FindMembers(ctx, actor, filter, page)
	if err != nil {
		return MembersPage{}, err
	}
	_ = cache.SetJSON(ctx, r.store, key, result, r.ttl)
	return result, nil
}

func (r *CachedReader) FindPTSessions(ctx context.Context, actor Actor, filter PTSessionFilter, page httpx.PageRequest) (PTSessionsPage, error) {
	key := cacheKey("pt-sessions", actor, filter.Status+"\x00"+filter.From+"\x00"+filter.To, page)
	var result PTSessionsPage
	if found, _ := cache.GetJSON(ctx, r.store, key, &result); found {
		return result, nil
	}
	result, err := r.next.FindPTSessions(ctx, actor, filter, page)
	if err != nil {
		return PTSessionsPage{}, err
	}
	_ = cache.SetJSON(ctx, r.store, key, result, r.ttl)
	return result, nil
}

func (r *CachedReader) FinanceSummary(ctx context.Context, actor Actor) (FinanceSummary, error) {
	key := cacheKey("finance-summary", actor, "", httpx.PageRequest{})
	var result FinanceSummary
	if found, _ := cache.GetJSON(ctx, r.store, key, &result); found {
		return result, nil
	}
	result, err := r.next.FinanceSummary(ctx, actor)
	if err != nil {
		return FinanceSummary{}, err
	}
	_ = cache.SetJSON(ctx, r.store, key, result, r.ttl)
	return result, nil
}

func (r *CachedReader) MobileDashboard(ctx context.Context, actor Actor) (MobileDashboard, error) {
	key := cacheKey("mobile-dashboard", actor, "", httpx.PageRequest{})
	var result MobileDashboard
	if found, _ := cache.GetJSON(ctx, r.store, key, &result); found {
		return result, nil
	}
	result, err := r.next.MobileDashboard(ctx, actor)
	if err != nil {
		return MobileDashboard{}, err
	}
	_ = cache.SetJSON(ctx, r.store, key, result, r.ttl)
	return result, nil
}

func cacheKey(kind string, actor Actor, filterKey string, page httpx.PageRequest) string {
	roles := append([]string(nil), actor.Roles...)
	values := url.Values{}
	values.Set("subject", actor.Subject)
	values.Set("roles", strings.Join(roles, ","))
	values.Set("filter", filterKey)
	values.Set("limit", strconv.Itoa(page.Limit))
	values.Set("cursor", page.Cursor)
	digest := sha256.Sum256([]byte(values.Encode()))
	return fmt.Sprintf("nexus:lf:%s:%s", kind, hex.EncodeToString(digest[:]))
}
