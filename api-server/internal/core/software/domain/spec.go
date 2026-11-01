package domain

import (
	"fmt"
	"reflect"
	"strings"
)

// Helpers for reading and normalizing an install spec, shared by the core and the Software modules.
// A spec arrives as decoded JSON (numbers are float64, arrays []any), from a composing launcher
// (int, []string), or read back from MongoDB (the driver's own slice type); every reader accepts
// all three.

// SpecString returns the string at key, or "" when it is absent or not a string.
func SpecString(spec map[string]any, key string) string {
	if spec == nil {
		return ""
	}
	if value, ok := spec[key].(string); ok {
		return value
	}
	return ""
}

// ValidateOptionalString accepts an absent or null key and otherwise requires a string.
func ValidateOptionalString(spec map[string]any, key string) error {
	if value, ok := spec[key]; ok && value != nil {
		if _, isString := value.(string); !isString {
			return fmt.Errorf("%w: spec.%s must be a string", ErrSpecInvalid, key)
		}
	}
	return nil
}

// SpecStringList reads an optional list of strings. Any slice is accepted, an entry that is not a
// string is ErrSpecInvalid, and entries are trimmed with empty ones dropped.
func SpecStringList(spec map[string]any, key string) ([]string, error) {
	value, present := spec[key]
	if !present || value == nil {
		return nil, nil
	}
	list := reflect.ValueOf(value)
	if list.Kind() != reflect.Slice {
		return nil, fmt.Errorf("%w: spec.%s must be an array of strings", ErrSpecInvalid, key)
	}
	out := make([]string, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		text, ok := list.Index(i).Interface().(string)
		if !ok {
			return nil, fmt.Errorf("%w: spec.%s must be an array of strings", ErrSpecInvalid, key)
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

// SpecPort reads an optional TCP port: a whole number in 1-65535, reporting whether it is present.
func SpecPort(spec map[string]any, key string) (int, bool, error) {
	value, present := spec[key]
	if !present || value == nil {
		return 0, false, nil
	}
	var port float64
	switch typed := value.(type) {
	case int:
		port = float64(typed)
	case int32:
		port = float64(typed)
	case int64:
		port = float64(typed)
	case float64:
		port = typed
	default:
		return 0, true, fmt.Errorf("%w: spec.%s must be a number", ErrSpecInvalid, key)
	}
	if port != float64(int(port)) || port < 1 || port > 65535 {
		return 0, true, fmt.Errorf("%w: spec.%s must be a whole number between 1 and 65535", ErrSpecInvalid, key)
	}
	return int(port), true, nil
}

// SetDefaultString stores fallback under key when the value is absent, not a string, or blank, and
// trims a present value.
func SetDefaultString(spec map[string]any, key, fallback string) {
	value := strings.TrimSpace(SpecString(spec, key))
	if value == "" {
		value = fallback
	}
	spec[key] = value
}

// SetIfPresent copies the trimmed string at specKey into vars under key when it is not blank.
func SetIfPresent(vars map[string]any, key string, spec map[string]any, specKey string) {
	if value := strings.TrimSpace(SpecString(spec, specKey)); value != "" {
		vars[key] = value
	}
}

// NonNilStrings keeps an empty list an empty array (not null) in trusted vars and stored specs.
func NonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// CloneSpec returns a shallow copy of spec, or nil for a nil spec.
func CloneSpec(spec map[string]any) map[string]any {
	if spec == nil {
		return nil
	}
	out := make(map[string]any, len(spec))
	for key, value := range spec {
		out[key] = value
	}
	return out
}

// KindList joins kinds for an error message ("docker-ce or podman").
func KindList(kinds []Kind) string {
	names := make([]string, len(kinds))
	for i, kind := range kinds {
		names[i] = string(kind)
	}
	return strings.Join(names, " or ")
}

// ValidateMethodAndVersion checks the optional install method, container runtime, and release
// version of a kind with several install methods. A runtime is only meaningful for the container
// method and must be one of the kind's container runtimes.
func ValidateMethodAndVersion(entry CatalogEntry, spec map[string]any) error {
	method := entry.InstallMethods[0]
	if value, present := spec[SpecInstallMethod]; present && value != nil {
		raw, ok := value.(string)
		if !ok || !entry.Allows(InstallMethod(strings.TrimSpace(raw))) {
			return fmt.Errorf("%w: spec.%s must be one of %v", ErrSpecInvalid, SpecInstallMethod, entry.InstallMethods)
		}
		method = InstallMethod(strings.TrimSpace(raw))
	}
	if value, present := spec[SpecContainerRuntime]; present && value != nil {
		raw, ok := value.(string)
		runtime := Kind(strings.TrimSpace(raw))
		switch {
		case !ok:
			return fmt.Errorf("%w: spec.%s must be a string", ErrSpecInvalid, SpecContainerRuntime)
		case runtime != "" && method != InstallMethodContainer:
			return fmt.Errorf("%w: spec.%s applies only to installMethod %s", ErrSpecInvalid, SpecContainerRuntime, InstallMethodContainer)
		case runtime != "" && !entry.RunsContainersOn(runtime):
			return fmt.Errorf("%w: spec.%s must be %s", ErrSpecInvalid, SpecContainerRuntime, KindList(entry.ContainerRuntimes))
		}
	}
	value, present := spec[SpecVersion]
	if !present || value == nil {
		return nil
	}
	raw, ok := value.(string)
	if !ok {
		return fmt.Errorf("%w: spec.%s must be a string", ErrSpecInvalid, SpecVersion)
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return ValidateReleaseVersion(NormalizeReleaseVersion(raw))
}

// NormalizeMethodAndVersion records the install method (the entry's default when omitted), drops a
// container runtime from a native install (the core resolves the container method's runtime when
// it plans the install), and records the release version, defaulting to defaultVersion, without a
// leading "v".
func NormalizeMethodAndVersion(entry CatalogEntry, spec map[string]any, defaultVersion string) {
	SetDefaultString(spec, SpecInstallMethod, string(entry.InstallMethods[0]))
	if InstallMethod(SpecString(spec, SpecInstallMethod)) != InstallMethodContainer {
		delete(spec, SpecContainerRuntime)
	} else {
		spec[SpecContainerRuntime] = strings.TrimSpace(SpecString(spec, SpecContainerRuntime))
	}
	SetDefaultString(spec, SpecVersion, defaultVersion)
	spec[SpecVersion] = NormalizeReleaseVersion(SpecString(spec, SpecVersion))
}
