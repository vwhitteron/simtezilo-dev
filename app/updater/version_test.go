package updater

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Version
		wantErr bool
	}{
		{
			name:  "simple version",
			input: "1.2.3",
			want:  &Version{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name:  "version with v prefix",
			input: "v1.2.3",
			want:  &Version{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name:  "version with prerelease",
			input: "1.2.3-beta.1",
			want:  &Version{Major: 1, Minor: 2, Patch: 3, PreRelease: "beta.1"},
		},
		{
			name:  "version with v prefix and prerelease",
			input: "v2.0.0-alpha",
			want:  &Version{Major: 2, Minor: 0, Patch: 0, PreRelease: "alpha"},
		},
		{
			name:    "invalid version",
			input:   "not-a-version",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "incomplete version",
			input:   "1.2",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersion() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if tt.wantErr {
				return
			}

			if got.Major != tt.want.Major || got.Minor != tt.want.Minor || got.Patch != tt.want.Patch {
				t.Errorf("ParseVersion() = %v, want %v", got, tt.want)
			}

			if got.PreRelease != tt.want.PreRelease {
				t.Errorf("ParseVersion() PreRelease = %v, want %v", got.PreRelease, tt.want.PreRelease)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want int
	}{
		{name: "equal versions", v1: "1.2.3", v2: "1.2.3", want: 0},
		{name: "major less than", v1: "1.0.0", v2: "2.0.0", want: -1},
		{name: "major greater than", v1: "2.0.0", v2: "1.0.0", want: 1},
		{name: "minor less than", v1: "1.1.0", v2: "1.2.0", want: -1},
		{name: "patch less than", v1: "1.2.3", v2: "1.2.4", want: -1},
		{name: "prerelease less than release", v1: "1.0.0-alpha", v2: "1.0.0", want: -1},
		{name: "release greater than prerelease", v1: "1.0.0", v2: "1.0.0-beta", want: 1},
		{name: "prerelease alpha less than beta", v1: "1.0.0-alpha", v2: "1.0.0-beta", want: -1},
		{name: "numeric prerelease comparison", v1: "1.0.0-beta.1", v2: "1.0.0-beta.2", want: -1},
		{name: "v prefix handled", v1: "v1.0.0", v2: "1.0.0", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := ParseVersion(tt.v1)
			if err != nil {
				t.Fatalf("Failed to parse v1: %v", err)
			}

			v2, err := ParseVersion(tt.v2)
			if err != nil {
				t.Fatalf("Failed to parse v2: %v", err)
			}

			got := v1.Compare(v2)
			if got != tt.want {
				t.Errorf("Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple version", input: "1.2.3", want: "v1.2.3"},
		{name: "with prerelease", input: "1.0.0-beta.1", want: "v1.0.0-beta.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := ParseVersion(tt.input)
			if got := v.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionLessThanGreaterThan(t *testing.T) {
	v1, _ := ParseVersion("1.0.0")
	v2, _ := ParseVersion("2.0.0")

	if !v1.LessThan(v2) {
		t.Error("Expected 1.0.0 < 2.0.0")
	}

	if v2.LessThan(v1) {
		t.Error("Expected 2.0.0 not < 1.0.0")
	}

	if !v2.GreaterThan(v1) {
		t.Error("Expected 2.0.0 > 1.0.0")
	}

	if v1.GreaterThan(v2) {
		t.Error("Expected 1.0.0 not > 2.0.0")
	}

	if !v1.Equal(v1) {
		t.Error("Expected 1.0.0 == 1.0.0")
	}
}
