// Package capability defines runtime capability values shared by application adapters.
package capability

type SupportLevel string

const (
	SupportOfficialLocal  SupportLevel = "official-local"
	SupportOfficialShared SupportLevel = "official-shared"
)

// Storage describes the operating envelope of the active storage adapter.
// It intentionally excludes driver names, connection strings and topology.
type Storage struct {
	SupportLevel  SupportLevel
	MultiInstance bool
}

// Provider exposes non-sensitive runtime capabilities to delivery adapters.
type Provider interface {
	StorageCapability() Storage
}
