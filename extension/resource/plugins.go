package resource

import (
	"net/url"
	"strings"
)

type Plugins struct {
	ChangeSet         *Endpoint `json:"changeSet,omitempty"`
	BindingCandidates *Endpoint `json:"bindingCandidates,omitempty"`
}

type Endpoint struct {
	URL string `json:"url"`
}

func (p *Plugins) Valid() bool {
	if p == nil {
		return true
	}
	for _, e := range []*Endpoint{p.ChangeSet, p.BindingCandidates} {
		if e == nil {
			continue
		}
		u, err := url.Parse(e.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
			strings.ContainsAny(e.URL, "?#") {
			return false
		}
	}
	return true
}
