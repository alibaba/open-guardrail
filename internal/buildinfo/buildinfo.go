// Package buildinfo contains metadata injected at build time.
package buildinfo

var Version = "dev"
var Commit = "unknown"

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"-"`
}

func Current() Info { return Info{Version: Version, Commit: Commit} }

type PublicInfo struct {
	Version string `json:"version"`
}

func Public() PublicInfo { return PublicInfo{Version: Version} }
