package domain

import (
	"errors"
	"strings"
	"testing"
)

// Validate enforces the swallow-owned rules of an upload intent and wraps every refusal in
// ErrInvalidOSImageUpload, so delivery maps each to 400.
func TestOSImageUploadValidate(t *testing.T) {
	valid := func() OSImageUpload {
		return OSImageUpload{
			IntegrationID: "integration-1",
			Name:          "rocky-10",
			Architecture:  "amd64",
			SizeBytes:     4,
			SHA256:        strings.Repeat("ab", 32),
		}
	}
	tests := []struct {
		name    string
		edit    func(*OSImageUpload)
		wantErr bool
	}{
		{name: "valid intent", edit: func(*OSImageUpload) {}},
		{name: "valid with default user", edit: func(u *OSImageUpload) { u.DefaultUser = "cloud-user" }},
		{name: "missing integration", edit: func(u *OSImageUpload) { u.IntegrationID = "" }, wantErr: true},
		{name: "missing name", edit: func(u *OSImageUpload) { u.Name = "" }, wantErr: true},
		{name: "missing architecture", edit: func(u *OSImageUpload) { u.Architecture = "" }, wantErr: true},
		{name: "title too long", edit: func(u *OSImageUpload) { u.Title = strings.Repeat("t", 201) }, wantErr: true},
		{name: "zero size", edit: func(u *OSImageUpload) { u.SizeBytes = 0 }, wantErr: true},
		{name: "short sha256", edit: func(u *OSImageUpload) { u.SHA256 = "abcd" }, wantErr: true},
		{name: "uppercase sha256", edit: func(u *OSImageUpload) { u.SHA256 = strings.Repeat("AB", 32) }, wantErr: true},
		{name: "invalid default user", edit: func(u *OSImageUpload) { u.DefaultUser = "Cloud User" }, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upload := valid()
			tc.edit(&upload)
			err := upload.Validate()
			if tc.wantErr != errors.Is(err, ErrInvalidOSImageUpload) || (!tc.wantErr && err != nil) {
				t.Errorf("Validate() error = %v, want invalid = %t", err, tc.wantErr)
			}
		})
	}
}
