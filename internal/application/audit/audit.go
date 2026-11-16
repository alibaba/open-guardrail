// Package audit exposes project-scoped reads of existing audit records.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxDetailsBytes bounds stored detail objects returned by audit readers.
const MaxDetailsBytes = 4096

var ErrInvalid = errors.New("invalid audit query")
var ErrUnavailable = errors.New("audit storage unavailable")

type Scope struct{ WorkspaceID, ProjectID string }
type Filters struct {
	EventType    string `json:"eventType,omitempty"`
	ActorID      string `json:"actorId,omitempty"`
	ResourceType string `json:"resourceType,omitempty"`
	ResourceID   string `json:"resourceId,omitempty"`
	From         *int64 `json:"from,omitempty"`
	To           *int64 `json:"to,omitempty"`
}
type Query struct {
	Filters
	Limit    int
	Cursor   string
	Redacted bool
}
type Event struct {
	ID                 string          `json:"id"`
	EventType          string          `json:"eventType"`
	ActorID            string          `json:"actorId,omitempty"`
	ActorRef           string          `json:"actorRef,omitempty"`
	OccurredAt         int64           `json:"occurredAt"`
	ResourceType       *string         `json:"resourceType,omitempty"`
	ResourceID         *string         `json:"resourceId,omitempty"`
	ResourceRef        string          `json:"resourceRef,omitempty"`
	Details            json.RawMessage `json:"details,omitempty"`
	DetailsDigest      string          `json:"detailsDigest,omitempty"`
	DetailsUnavailable bool            `json:"detailsUnavailable,omitempty"`
}
type Page struct {
	Items      []Event `json:"items"`
	NextCursor *string `json:"nextCursor,omitempty"`
	Redacted   bool    `json:"redacted"`
}
type Repository interface {
	ListAuditEvents(context.Context, Scope, Filters, int, int64) ([]Event, bool, error)
}
type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }

type cursor struct {
	Before int64  `json:"before"`
	Filter string `json:"filter"`
}

func (s *Service) List(ctx context.Context, scope Scope, q Query) (Page, error) {
	if scope.WorkspaceID == "" || scope.ProjectID == "" || q.Limit < 1 || q.Limit > 100 {
		return Page{}, ErrInvalid
	}
	for _, value := range []string{q.EventType, q.ActorID, q.ResourceType, q.ResourceID} {
		if !utf8.ValidString(value) || len(value) > 512 || strings.ContainsFunc(value, unicode.IsControl) {
			return Page{}, ErrInvalid
		}
	}
	if (q.From != nil && *q.From < 0) || (q.To != nil && *q.To < 0) || (q.From != nil && q.To != nil && *q.From >= *q.To) {
		return Page{}, ErrInvalid
	}
	// Bind the opaque cursor to both the query and its authenticated project.
	encoded, _ := json.Marshal(struct {
		Scope   Scope
		Filters Filters
	}{scope, q.Filters})
	filter := fingerprint(encoded)
	var before int64
	if q.Cursor != "" {
		if len(q.Cursor) > 2048 {
			return Page{}, ErrInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		var c cursor
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Before <= 0 || c.Filter != filter {
			return Page{}, ErrInvalid
		}
		before = c.Before
	}
	items, more, err := s.repository.ListAuditEvents(ctx, scope, q.Filters, q.Limit, before)
	if err != nil {
		return Page{}, err
	}
	if items == nil {
		items = []Event{}
	}
	page := Page{Items: append([]Event{}, items...), Redacted: q.Redacted}
	if more && len(items) > 0 {
		id, err := strconv.ParseInt(items[len(items)-1].ID, 10, 64)
		if err != nil || id <= 0 {
			return Page{}, ErrUnavailable
		}
		raw, _ := json.Marshal(cursor{Before: id, Filter: filter})
		next := base64.RawURLEncoding.EncodeToString(raw)
		page.NextCursor = &next
	}
	for i := range page.Items {
		event := &page.Items[i]
		if len(event.Details) > MaxDetailsBytes {
			event.Details = nil
			event.DetailsUnavailable = true
		}
		if len(event.Details) > 0 {
			event.DetailsDigest = fingerprint(event.Details)
			if !json.Valid(event.Details) || !strings.HasPrefix(strings.TrimSpace(string(event.Details)), "{") {
				event.Details = nil
				event.DetailsUnavailable = true
			}
		}
		if q.Redacted {
			if event.ActorID != "" {
				event.ActorRef = fingerprint([]byte(event.ActorID))
			}
			event.ActorID = ""
			if event.ResourceID != nil {
				event.ResourceRef = fingerprint([]byte(*event.ResourceID))
				event.ResourceID = nil
			}
			event.Details = nil
		}
	}
	return page, nil
}
func fingerprint(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
