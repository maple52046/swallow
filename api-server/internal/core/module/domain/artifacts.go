package domain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// The artifact formats a source may have. They name a kind of content, never the product that
// serves or installs it (core-and-extensions.md §2), so supporting another product changes an
// adapter, not these values. Modules declare only deb sources today.
const (
	ArtifactFormatDeb    = "deb"
	ArtifactFormatRPM    = "rpm"
	ArtifactFormatOCI    = "oci"
	ArtifactFormatPython = "python"
	ArtifactFormatFile   = "file"
)

var artifactFormats = map[string]bool{
	ArtifactFormatDeb: true, ArtifactFormatRPM: true, ArtifactFormatOCI: true,
	ArtifactFormatPython: true, ArtifactFormatFile: true,
}

// ErrInvalidArtifact means an artifact declaration breaks the rules Artifacts.Validate states. A
// manifest that carries one also fails with ErrInvalidManifest.
var ErrInvalidArtifact = errors.New("invalid artifact declaration")

// Artifacts is everything a module needs from its Site's Local Repository to deploy without the
// internet: the upstream package sources to mirror, the single files to fetch and verify, and the
// container images and Python requirements to fetch before a deployment needs them. A role fetches
// only declared artifacts, and only from the Local Repository. OS packages are not declared here:
// they come from the Site's OS source, which the Site's user declares.
//
// Validate checks one declaration on its own; NewRegistry also checks the declarations of different
// modules against each other, because the Local Repository holds one copy of each source and file.
type Artifacts struct {
	// Sources are upstream package repositories the Local Repository mirrors, each by a name unique
	// within the Site's Local Repository.
	Sources []ArtifactSource `json:"sources,omitempty"`
	// Files are single files the Local Repository stores at their paths.
	Files []FileArtifact `json:"files,omitempty"`
	// Images are fully qualified container image references: a registry host, a repository, and a
	// tag, a sha256 digest, or both.
	Images []string `json:"images,omitempty"`
	// Python are pinned requirements, "name==version".
	Python []string `json:"python,omitempty"`
}

// ArtifactSource is one upstream package repository. Name identifies the source in the Local
// Repository and in what hosts are configured with, so two declarations of one name must be the
// same repository: the same Format and URL. Suites and Components select what a deb source mirrors
// and are required there and refused elsewhere; Architectures narrow a deb or rpm source.
// Declarations of one source may select different suites, components, and architectures, and the
// consumer mirrors their union.
type ArtifactSource struct {
	Name          string   `json:"name"`
	Format        string   `json:"format"`
	URL           string   `json:"url"`
	Suites        []string `json:"suites,omitempty"`
	Components    []string `json:"components,omitempty"`
	Architectures []string `json:"architectures,omitempty"`
}

// FileArtifact is one file fetched from URL and stored at Path under the Local Repository's file
// root, where roles read it. SHA256 is the lowercase hex digest the stored file must have; it is the
// file's identity, so a file whose content changes upstream fails verification instead of reaching
// hosts.
type FileArtifact struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// DeclaredArtifacts is one module's declaration as Registry.Artifacts reports it. ModuleID is the
// manifest's id (not its "<kind>/<id>" reference), by which a consumer attributes a source or file
// to the modules that declare it.
type DeclaredArtifacts struct {
	ModuleID  string
	Artifacts Artifacts
}

var (
	artifactNamePattern  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	artifactTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+/-]*$`)
	// A path is relative, slash-separated, and uses only characters that need no escaping in the
	// URL a role builds by appending it to the Local Repository's file root.
	filePathPattern = regexp.MustCompile(`^[A-Za-z0-9._+~-]+(/[A-Za-z0-9._+~-]+)*$`)
	sha256Pattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	// imageReferencePattern requires the registry host a short reference leaves implicit: a dotted
	// domain or localhost, with an optional port, or a single label with a port. The repository
	// components follow the distribution reference grammar; the tag and the digest are the two
	// submatches.
	imageReferencePattern = regexp.MustCompile(`^` +
		`(?:(?:localhost|[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+)(?::[0-9]+)?|[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?:[0-9]+)` +
		`/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*` +
		`(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(@sha256:[a-f0-9]{64})?$`)
	pythonRequirementPattern = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)==[0-9](?:[A-Za-z0-9.!+_-]*[A-Za-z0-9])?$`)
	pythonNameSeparators     = regexp.MustCompile(`[-_.]+`)
)

// Validate checks one declaration on its own and returns an error wrapping ErrInvalidArtifact that
// names the first entry that breaks a rule:
//
//   - a source has a kebab-case name, a known format, and a URL as checkArtifactURL accepts; a deb
//     source names at least one suite and one component;
//   - a file has a relative path without "." or ".." elements, a URL as checkArtifactURL accepts,
//     and a lowercase hex SHA-256;
//   - an image names its registry host and a tag or a sha256 digest;
//   - a Python requirement pins one version with "==".
//
// Within the declaration, a source name, a file path, an image, and a Python project (compared by
// its normalized name) each appear once.
func (a Artifacts) Validate() error {
	sourceNames := make([]string, 0, len(a.Sources))
	for _, source := range a.Sources {
		if err := source.validate(); err != nil {
			return err
		}
		sourceNames = append(sourceNames, source.Name)
	}
	if duplicate := firstDuplicate(sourceNames); duplicate != "" {
		return fmt.Errorf("%w: source %q is declared twice", ErrInvalidArtifact, duplicate)
	}
	paths := make([]string, 0, len(a.Files))
	for _, file := range a.Files {
		if err := file.validate(); err != nil {
			return err
		}
		paths = append(paths, file.Path)
	}
	if duplicate := firstDuplicate(paths); duplicate != "" {
		return fmt.Errorf("%w: file %q is declared twice", ErrInvalidArtifact, duplicate)
	}
	for _, image := range a.Images {
		if match := imageReferencePattern.FindStringSubmatch(image); match == nil || (match[1] == "" && match[2] == "") {
			return fmt.Errorf("%w: image %q is not host/repository with a tag or a sha256 digest", ErrInvalidArtifact, image)
		}
	}
	if duplicate := firstDuplicate(a.Images); duplicate != "" {
		return fmt.Errorf("%w: image %q is declared twice", ErrInvalidArtifact, duplicate)
	}
	projects := make([]string, 0, len(a.Python))
	for _, requirement := range a.Python {
		match := pythonRequirementPattern.FindStringSubmatch(requirement)
		if match == nil {
			return fmt.Errorf("%w: Python requirement %q must pin one version as name==version", ErrInvalidArtifact, requirement)
		}
		projects = append(projects, normalizePythonName(match[1]))
	}
	if duplicate := firstDuplicate(projects); duplicate != "" {
		return fmt.Errorf("%w: Python project %q is pinned twice", ErrInvalidArtifact, duplicate)
	}
	return nil
}

func (s ArtifactSource) validate() error {
	if !artifactNamePattern.MatchString(s.Name) {
		return fmt.Errorf("%w: source name %q must be kebab-case", ErrInvalidArtifact, s.Name)
	}
	if !artifactFormats[s.Format] {
		return fmt.Errorf("%w: source %s format %q is not deb, rpm, oci, python, or file", ErrInvalidArtifact, s.Name, s.Format)
	}
	if err := checkArtifactURL(s.URL); err != nil {
		return fmt.Errorf("%w: source %s: %v", ErrInvalidArtifact, s.Name, err)
	}
	deb := s.Format == ArtifactFormatDeb
	if deb && (len(s.Suites) == 0 || len(s.Components) == 0) {
		return fmt.Errorf("%w: deb source %s must name its suites and components", ErrInvalidArtifact, s.Name)
	}
	if !deb && (len(s.Suites) > 0 || len(s.Components) > 0) {
		return fmt.Errorf("%w: source %s: only a deb source has suites and components", ErrInvalidArtifact, s.Name)
	}
	if !deb && s.Format != ArtifactFormatRPM && len(s.Architectures) > 0 {
		return fmt.Errorf("%w: source %s: only a deb or rpm source has architectures", ErrInvalidArtifact, s.Name)
	}
	for field, values := range map[string][]string{
		"suite": s.Suites, "component": s.Components, "architecture": s.Architectures,
	} {
		for _, value := range values {
			if !artifactTokenPattern.MatchString(value) {
				return fmt.Errorf("%w: source %s %s %q is not a single word", ErrInvalidArtifact, s.Name, field, value)
			}
		}
		if duplicate := firstDuplicate(values); duplicate != "" {
			return fmt.Errorf("%w: source %s lists %s %q twice", ErrInvalidArtifact, s.Name, field, duplicate)
		}
	}
	return nil
}

func (f FileArtifact) validate() error {
	if !filePathPattern.MatchString(f.Path) {
		return fmt.Errorf("%w: file path %q must be relative, slash-separated, and plain", ErrInvalidArtifact, f.Path)
	}
	for _, element := range strings.Split(f.Path, "/") {
		if element == "." || element == ".." {
			return fmt.Errorf("%w: file path %q must not contain %q", ErrInvalidArtifact, f.Path, element)
		}
	}
	if err := checkArtifactURL(f.URL); err != nil {
		return fmt.Errorf("%w: file %s: %v", ErrInvalidArtifact, f.Path, err)
	}
	if !sha256Pattern.MatchString(f.SHA256) {
		return fmt.Errorf("%w: file %s sha256 must be 64 lowercase hex digits", ErrInvalidArtifact, f.Path)
	}
	return nil
}

// checkArtifactURL accepts an absolute http or https URL with a host. A declaration is shipped
// with the release and fetched by the Local Repository as is, so it must not carry a credential
// (user info) or anything a fetch would not send as a path (a query or a fragment).
func checkArtifactURL(raw string) error {
	if strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return fmt.Errorf("url %q contains whitespace", raw)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url %q: %v", raw, err)
	}
	switch {
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return fmt.Errorf("url %q must be http or https", raw)
	case parsed.Host == "":
		return fmt.Errorf("url %q has no host", raw)
	case parsed.User != nil:
		return fmt.Errorf("url %q must not carry user info", raw)
	case parsed.RawQuery != "" || parsed.ForceQuery || strings.ContainsRune(raw, '#'):
		return fmt.Errorf("url %q must not carry a query or a fragment", raw)
	}
	return nil
}

// normalizePythonName is the project name as package indexes compare it: case-insensitive, with
// runs of "-", "_", and "." equal.
func normalizePythonName(name string) string {
	return pythonNameSeparators.ReplaceAllString(strings.ToLower(name), "-")
}

func (a Artifacts) empty() bool {
	return len(a.Sources) == 0 && len(a.Files) == 0 && len(a.Images) == 0 && len(a.Python) == 0
}

func (a Artifacts) clone() Artifacts {
	cloned := Artifacts{Images: slices.Clone(a.Images), Python: slices.Clone(a.Python), Files: slices.Clone(a.Files)}
	for _, source := range a.Sources {
		source.Suites = slices.Clone(source.Suites)
		source.Components = slices.Clone(source.Components)
		source.Architectures = slices.Clone(source.Architectures)
		cloned.Sources = append(cloned.Sources, source)
	}
	return cloned
}

// artifactClaims records, while a registry is built, the first module to declare each source name
// and file path, so that a later declaration of the same name is checked against it.
type artifactClaims struct {
	sources map[string]claimedSource
	files   map[string]claimedFile
}

type claimedSource struct {
	owner  string
	source ArtifactSource
}

type claimedFile struct {
	owner string
	file  FileArtifact
}

func newArtifactClaims() artifactClaims {
	return artifactClaims{sources: map[string]claimedSource{}, files: map[string]claimedFile{}}
}

// claim adds the declaration of module ref, failing with ErrModuleConflict when a source name it
// shares with an earlier module names another format or URL, or a file path it shares names another
// URL or digest.
func (c artifactClaims) claim(ref string, artifacts Artifacts) error {
	for _, source := range artifacts.Sources {
		first, exists := c.sources[source.Name]
		if !exists {
			c.sources[source.Name] = claimedSource{owner: ref, source: source}
			continue
		}
		if first.source.Format != source.Format || first.source.URL != source.URL {
			return fmt.Errorf("%w: source %q is %s %s in %s but %s %s in %s", ErrModuleConflict, source.Name,
				first.source.Format, first.source.URL, first.owner, source.Format, source.URL, ref)
		}
	}
	for _, file := range artifacts.Files {
		first, exists := c.files[file.Path]
		if !exists {
			c.files[file.Path] = claimedFile{owner: ref, file: file}
			continue
		}
		if first.file != file {
			return fmt.Errorf("%w: file %q has a different URL or sha256 in %s and %s", ErrModuleConflict, file.Path, first.owner, ref)
		}
	}
	return nil
}

// Artifacts returns each module's artifact declaration, ordered by module id; a module that
// declares nothing is left out. NewRegistry has validated every declaration and their agreement, so
// a consumer may merge the declarations of a shared source (the union of its suites, components, and
// architectures) and store a shared file once. The result is a copy the caller may change.
func (r *Registry) Artifacts() []DeclaredArtifacts {
	var declared []DeclaredArtifacts
	for _, module := range r.modules {
		if module.Manifest.Artifacts.empty() {
			continue
		}
		declared = append(declared, DeclaredArtifacts{ModuleID: module.Manifest.ID, Artifacts: module.Manifest.Artifacts.clone()})
	}
	sort.SliceStable(declared, func(i, j int) bool { return declared[i].ModuleID < declared[j].ModuleID })
	return declared
}
