// Package resolver resolves the bounded, authored FixtureVersionV1 catalog.
// These types do not represent Fabric or provider metadata formats.
package resolver

import "modgrid.local/modgrid/internal/document"

const (
	MaxDocumentBytes = 4 << 20
	MaxArtifacts     = 256
	MaxProjects      = 64
	MaxEdges         = 1024
	MaxDecisions     = 10000
)

type Error = document.Error

func failure(code, message string) *Error {
	return document.Fail(code, message)
}

type Loader struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type Target struct {
	Minecraft string `json:"minecraft"`
	Loader    Loader `json:"loader"`
	JavaMajor int    `json:"java_major"`
	Platform  string `json:"platform"`
	Side      string `json:"side"`
}

type Request struct {
	Project    string   `json:"project"`
	Constraint string   `json:"constraint"`
	Sides      []string `json:"sides"`
	Optional   bool     `json:"optional"`
}

type Choice struct {
	Project string `json:"project"`
	Include bool   `json:"include"`
}

type Config struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type PackSpec struct {
	Schema        string    `json:"schema"`
	ID            string    `json:"id"`
	CatalogID     string    `json:"catalog_id"`
	CatalogSHA256 string    `json:"catalog_sha256"`
	Target        Target    `json:"target"`
	Roots         []Request `json:"roots"`
	Choices       []Choice  `json:"choices"`
	Configs       []Config  `json:"configs"`
}

type Sides struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

type Dependency struct {
	Project    string `json:"project"`
	Constraint string `json:"constraint"`
	Optional   bool   `json:"optional"`
	Original   string `json:"original"`
}

type Rights struct {
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

type Source struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Facts    string `json:"facts"`
}

type Artifact struct {
	Project       string       `json:"project"`
	Version       string       `json:"version"`
	VersionID     string       `json:"version_id"`
	FileID        string       `json:"file_id"`
	Source        Source       `json:"source"`
	Size          int64        `json:"size_bytes"`
	SHA512        string       `json:"sha512"`
	Minecraft     string       `json:"minecraft"`
	Loader        Loader       `json:"loader"`
	Compatibility string       `json:"compatibility_evidence"`
	Sides         Sides        `json:"sides"`
	OriginalSides string       `json:"original_sides"`
	Dependencies  []Dependency `json:"dependencies"`
	License       string       `json:"license"`
	Rights        Rights       `json:"rights"`
}

type Catalog struct {
	Schema    string     `json:"schema"`
	ID        string     `json:"id"`
	Artifacts []Artifact `json:"artifacts"`
}

type Reason struct {
	From       string `json:"from"`
	Project    string `json:"project"`
	Constraint string `json:"constraint"`
}

type Lock struct {
	Schema    string     `json:"schema"`
	Pack      PackSpec   `json:"pack"`
	Artifacts []Artifact `json:"artifacts"`
	Reasons   []Reason   `json:"reasons"`
}
