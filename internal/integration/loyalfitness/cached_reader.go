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
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &CachedReader{next: next, store: store, ttl: ttl}
}

func (r *CachedReader) FindMembers(ctx context.Context, actor Actor, filter MemberFilter, page httpx.PageRequest) (MembersPage, error) {
	if r.next == nil {
		return MembersPage{}, ErrReaderUnavailable
	}
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
	key := cacheKey("members", actor, filter.Query+"\x00"+filter.Status, page)
	var result MembersPage
	if r.store != nil {
		if found, _ := cache.GetJSON(ctx, r.store, key, &result); found {
			return result, nil
		}
	}
	result, err = r.next.FindMembers(ctx, actor, filter, page)
	if err != nil {
		return MembersPage{}, err
	}
	if r.store != nil {
		_ = cache.SetJSON(ctx, r.store, key, result, r.ttl)
	}
	return result, nil
}

func (r *CachedReader) FindPTSessions(ctx context.Context, actor Actor, filter PTSessionFilter, page httpx.PageRequest) (PTSessionsPage, error) {
	if r.next == nil {
		return PTSessionsPage{}, ErrReaderUnavailable
	}
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
	key := cacheKey("pt-sessions", actor, filter.Status+"\x00"+filter.From+"\x00"+filter.To, page)
	var result PTSessionsPage
	if r.store != nil {
		if found, _ := cache.GetJSON(ctx, r.store, key, &result); found {
			return result, nil
		}
	}
	result, err = r.next.FindPTSessions(ctx, actor, filter, page)
	if err != nil {
		return PTSessionsPage{}, err
	}
	if r.store != nil {
		_ = cache.SetJSON(ctx, r.store, key, result, r.ttl)
	}
	return result, nil
}

func (r *CachedReader) FinanceSummary(ctx context.Context, actor Actor) (FinanceSummary, error) {
	if r.next == nil {
		return FinanceSummary{}, ErrReaderUnavailable
	}
	if err := validateActor(actor); err != nil {
		return FinanceSummary{}, err
	}
	key := cacheKey("finance-summary", actor, "", httpx.PageRequest{})
	var result FinanceSummary
	if r.store != nil {
		if found, _ := cache.GetJSON(ctx, r.store, key, &result); found {
			return result, nil
		}
	}
	result, err := r.next.FinanceSummary(ctx, actor)
	if err != nil {
		return FinanceSummary{}, err
	}
	if r.store != nil {
		_ = cache.SetJSON(ctx, r.store, key, result, r.ttl)
	}
	return result, nil
}

func (r *CachedReader) MobileDashboard(ctx context.Context, actor Actor) (MobileDashboard, error) {
	if r.next == nil {
		return MobileDashboard{}, ErrReaderUnavailable
	}
	if err := validateActor(actor); err != nil {
		return MobileDashboard{}, err
	}
	key := cacheKey("mobile-dashboard", actor, "", httpx.PageRequest{})
	var result MobileDashboard
	if r.store != nil {
		if found, _ := cache.GetJSON(ctx, r.store, key, &result); found {
			return result, nil
		}
	}
	result, err := r.next.MobileDashboard(ctx, actor)
	if err != nil {
		return MobileDashboard{}, err
	}
	if r.store != nil {
		_ = cache.SetJSON(ctx, r.store, key, result, r.ttl)
	}
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
