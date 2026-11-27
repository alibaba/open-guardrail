package spec

import (
	"context"
	"regexp"
	"slices"
)

const MaxMaintainers = 100

var maintainerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:@-]{0,127}$`)

func ValidActorID(id string) bool {
	return maintainerID.MatchString(id)
}

func ValidMaintainers(ids []string) bool {
	if len(ids) == 0 || len(ids) > MaxMaintainers {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !ValidActorID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func (p Policy) CanMaintain(actor string) bool {
	return actor != "" && slices.Contains(p.Maintainers, actor)
}

// The repository checks the current list while holding the Policy write lock.
func (s *Service) SetPolicyMaintainers(ctx context.Context, scope Scope, id string, maintainers []string) (Policy, error) {
	if !ValidMaintainers(maintainers) {
		return Policy{}, ErrInvalid
	}
	maintainers = slices.Clone(maintainers)
	slices.Sort(maintainers)
	return s.repository.SetPolicyMaintainers(ctx, scope, id, maintainers)
}
