package application

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// pythonSourceName and pythonSourceURL are the Repository Source the Python requirements are pulled
// through: the public Python package index, which a module's `python` requirements name packages of.
const (
	pythonSourceName = "pypi"
	pythonSourceURL  = "https://pypi.org/"
)

// EffectiveSource is one Repository Source the Site's repository must serve, and who asked for it:
// DeclaredBy holds localrepodomain.DeclaredByModule values and DeclaredByUser, in declaration order.
type EffectiveSource struct {
	localrepodomain.Source
	DeclaredBy []string
}

// EffectiveFile is one File the Site's repository must serve, and the modules that declared it.
type EffectiveFile struct {
	localrepodomain.File
	DeclaredBy []string
}

// EffectiveImage is one container image a module declared; it is fetched through the pull-through
// source of its registry host.
type EffectiveImage struct {
	Reference  string
	DeclaredBy []string
}

// EffectivePython is one Python requirement a module declared; it is fetched through the pull-through
// Python source.
type EffectivePython struct {
	Requirement string
	DeclaredBy  []string
}

// Conflict is a declaration the effective spec leaves out because it contradicts one made before
// it: Name is the source name or file path, Kept the declaredBy values of the declaration that
// stays, LeftOut those of the one left out, and Reason the contradiction.
type Conflict struct {
	Name    string
	Kept    []string
	LeftOut []string
	Reason  string
}

// InvolvesUser reports whether the user's spec is on either side, so a spec write that would
// cause it is refused.
func (c Conflict) InvolvesUser() bool {
	return slices.Contains(c.Kept, localrepodomain.DeclaredByUser) || slices.Contains(c.LeftOut, localrepodomain.DeclaredByUser)
}

// Message is a sentence naming both sides, for example "source docker-ce of user has URL
// https://mirror.example, which contradicts its declaration by module:docker-ce".
func (c Conflict) Message() string {
	return fmt.Sprintf("%s of %s %s, which contradicts its declaration by %s",
		c.Name, strings.Join(c.LeftOut, ", "), c.Reason, strings.Join(c.Kept, ", "))
}

// EffectiveSpec is what a Site's Local Repository must hold: every module declaration merged with
// the Site's spec. Each list is ordered by name (sources), path, reference, or requirement.
type EffectiveSpec struct {
	Sources   []EffectiveSource
	Files     []EffectiveFile
	Images    []EffectiveImage
	Python    []EffectivePython
	Conflicts []Conflict
}

// Effective merges the modules' declarations (in the given order) with the user's sources and adds
// the implicit pull-through sources. The rules:
//
//   - Sources merge by name: the first declaration fixes the format and URL, and a later one with
//     the same format and URL adds its suites, components, and architectures (a union in order of
//     appearance) and its declarer. One with another format or URL is a Conflict and left out.
//   - Files merge by path; a later one with another URL or checksum is a Conflict and left out.
//   - Images and Python requirements merge by their text.
//   - Each registry host an image names gets an oci source named after the host, at
//     "https://<host>", declared by the images' declarers; a declared source of that name and format
//     keeps its own URL (a mirror of the registry), and one of another format is kept and the
//     implicit source is a Conflict. Likewise any Python requirement adds the python source "pypi".
//
// An image reference whose first path element is not a registry host is not fully qualified, and
// gets no implicit source.
func Effective(modules []localrepodomain.ModuleDeclaration, user []localrepodomain.Source) EffectiveSpec {
	merger := newMerger()
	for _, module := range modules {
		by := localrepodomain.DeclaredByModule(module.ModuleID)
		for _, source := range module.Sources {
			merger.addSource(source, []string{by}, false)
		}
		for _, file := range module.Files {
			merger.addFile(file, by)
		}
		for _, reference := range module.Images {
			merger.images.add(reference, by)
		}
		for _, requirement := range module.Python {
			merger.python.add(requirement, by)
		}
	}
	for _, source := range user {
		merger.addSource(source, []string{localrepodomain.DeclaredByUser}, false)
	}
	hosts := map[string][]string{}
	var hostOrder []string
	for _, reference := range merger.images.order {
		host, ok := RegistryHost(reference)
		if !ok {
			continue
		}
		if _, seen := hosts[host]; !seen {
			hostOrder = append(hostOrder, host)
		}
		hosts[host] = appendUnique(hosts[host], merger.images.by[reference]...)
	}
	for _, host := range hostOrder {
		merger.addSource(localrepodomain.Source{Name: host, Format: localrepodomain.FormatOCI, URL: "https://" + host}, hosts[host], true)
	}
	if len(merger.python.order) > 0 {
		var by []string
		for _, requirement := range merger.python.order {
			by = appendUnique(by, merger.python.by[requirement]...)
		}
		merger.addSource(localrepodomain.Source{Name: pythonSourceName, Format: localrepodomain.FormatPython, URL: pythonSourceURL}, by, true)
	}
	return merger.spec()
}

// RegistryHost returns the registry host of a fully qualified image reference: its first path
// element, when that names a host (it has a dot or a port, or is "localhost").
func RegistryHost(reference string) (string, bool) {
	host, rest, found := strings.Cut(reference, "/")
	if !found || rest == "" || host == "" {
		return "", false
	}
	if host != "localhost" && !strings.ContainsAny(host, ".:") {
		return "", false
	}
	return host, true
}

// merger accumulates declarations in order; spec returns the merged result.
type merger struct {
	sources     map[string]*EffectiveSource
	sourceOrder []string
	files       map[string]*EffectiveFile
	fileOrder   []string
	images      *declaredTexts
	python      *declaredTexts
	conflicts   []Conflict
}

func newMerger() *merger {
	return &merger{
		sources: map[string]*EffectiveSource{}, files: map[string]*EffectiveFile{},
		images: newDeclaredTexts(), python: newDeclaredTexts(),
	}
}

// addSource merges one source declaration. implicit marks a source swallow derives from images or
// Python requirements, which defers to a declared source of the same name and format.
func (m *merger) addSource(source localrepodomain.Source, by []string, implicit bool) {
	current, exists := m.sources[source.Name]
	if !exists {
		copied := EffectiveSource{Source: cloneSource(source), DeclaredBy: append([]string(nil), by...)}
		m.sources[source.Name] = &copied
		m.sourceOrder = append(m.sourceOrder, source.Name)
		return
	}
	switch {
	case current.Format != source.Format:
		m.conflicts = append(m.conflicts, Conflict{
			Name: "source " + source.Name, Kept: slices.Clone(current.DeclaredBy), LeftOut: slices.Clone(by),
			Reason: fmt.Sprintf("has format %s", source.Format),
		})
		return
	case !implicit && !sameURL(current.URL, source.URL):
		m.conflicts = append(m.conflicts, Conflict{
			Name: "source " + source.Name, Kept: slices.Clone(current.DeclaredBy), LeftOut: slices.Clone(by),
			Reason: fmt.Sprintf("has URL %s", source.URL),
		})
		return
	}
	current.Suites = appendUnique(current.Suites, source.Suites...)
	current.Components = appendUnique(current.Components, source.Components...)
	current.Architectures = appendUnique(current.Architectures, source.Architectures...)
	current.DeclaredBy = appendUnique(current.DeclaredBy, by...)
}

// addFile merges one file declaration; the checksum compares case-insensitively because it is hex.
func (m *merger) addFile(file localrepodomain.File, by string) {
	current, exists := m.files[file.Path]
	if !exists {
		m.files[file.Path] = &EffectiveFile{File: file, DeclaredBy: []string{by}}
		m.fileOrder = append(m.fileOrder, file.Path)
		return
	}
	if current.URL != file.URL || !strings.EqualFold(current.SHA256, file.SHA256) {
		m.conflicts = append(m.conflicts, Conflict{
			Name: "file " + file.Path, Kept: slices.Clone(current.DeclaredBy), LeftOut: []string{by},
			Reason: "has another URL or checksum",
		})
		return
	}
	current.DeclaredBy = appendUnique(current.DeclaredBy, by)
}

// spec returns the merged declarations in EffectiveSpec's order; conflicts keep declaration order.
func (m *merger) spec() EffectiveSpec {
	var spec EffectiveSpec
	names := slices.Clone(m.sourceOrder)
	sort.Strings(names)
	for _, name := range names {
		spec.Sources = append(spec.Sources, *m.sources[name])
	}
	paths := slices.Clone(m.fileOrder)
	sort.Strings(paths)
	for _, path := range paths {
		spec.Files = append(spec.Files, *m.files[path])
	}
	for _, reference := range m.images.sorted() {
		spec.Images = append(spec.Images, EffectiveImage{Reference: reference, DeclaredBy: m.images.by[reference]})
	}
	for _, requirement := range m.python.sorted() {
		spec.Python = append(spec.Python, EffectivePython{Requirement: requirement, DeclaredBy: m.python.by[requirement]})
	}
	spec.Conflicts = m.conflicts
	return spec
}

// declaredTexts collects declared strings (image references, Python requirements) with their
// declarers.
type declaredTexts struct {
	order []string
	by    map[string][]string
}

func newDeclaredTexts() *declaredTexts {
	return &declaredTexts{by: map[string][]string{}}
}

func (d *declaredTexts) add(text, by string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if _, seen := d.by[text]; !seen {
		d.order = append(d.order, text)
	}
	d.by[text] = appendUnique(d.by[text], by)
}

func (d *declaredTexts) sorted() []string {
	out := slices.Clone(d.order)
	sort.Strings(out)
	return out
}

// sameURL compares two upstream URLs, ignoring one trailing slash, which names the same location.
func sameURL(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

// appendUnique appends the values of add that list does not hold yet, keeping order.
func appendUnique(list []string, add ...string) []string {
	for _, value := range add {
		if !slices.Contains(list, value) {
			list = append(list, value)
		}
	}
	return list
}

func cloneSource(source localrepodomain.Source) localrepodomain.Source {
	source.Suites = slices.Clone(source.Suites)
	source.Components = slices.Clone(source.Components)
	source.Architectures = slices.Clone(source.Architectures)
	return source
}
