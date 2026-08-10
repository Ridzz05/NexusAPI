package loyalfitness

import (
	"context"

	"github.com/Ridzz05/NexusAPI/internal/platform/httpx"
)

// Reader is the seam for the first incremental Loyal Fitness migration. An
// implementation can read from Laravel, a replicated PostgreSQL schema, or a
// dedicated read model without changing the public transport contract.
type Reader interface {
	FindMembers(context.Context, Actor, MemberFilter, httpx.PageRequest) (MembersPage, error)
	FindPTSessions(context.Context, Actor, PTSessionFilter, httpx.PageRequest) (PTSessionsPage, error)
	FinanceSummary(context.Context, Actor) (FinanceSummary, error)
	MobileDashboard(context.Context, Actor) (MobileDashboard, error)
}

type MemberFilter struct {
	Query  string
	Status string
}

type PTSessionFilter struct {
	Status string
	From   string
	To     string
}

type Actor struct {
	Subject string
	Roles   []string
}

func (a Actor) CanViewAllMembers() bool {
	for _, role := range a.Roles {
		switch role {
		case "admin", "owner", "manager", "staff":
			return true
		}
	}
	return false
}

func (a Actor) CanAccessSubject(subject string) bool {
	return subject == a.Subject || a.CanViewAllMembers()
}

type MembersPage struct {
	Items      []Member
	NextCursor string
	HasMore    bool
}

type Member struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

type PTSessionsPage struct {
	Items      []PTSession
	NextCursor string
	HasMore    bool
}

type PTSession struct {
	ID       string `json:"id"`
	MemberID string `json:"member_id"`
	StartsAt string `json:"starts_at"`
	Status   string `json:"status"`
}

type FinanceSummary struct {
	Period   string `json:"period"`
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
}

type MobileDashboard struct {
	MemberCount int `json:"member_count"`
	UpcomingPT  int `json:"upcoming_pt_sessions"`
}
