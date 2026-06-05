package audio

import (
	"errors"
	"testing"
)

// fakeBackend is a Backend whose only meaningful behaviour is ListDevices,
// which ResolveOutputDevice depends on.
type fakeBackend struct {
	devices []Device
	err     error
}

func (f *fakeBackend) Name() string                   { return "fake" }
func (f *fakeBackend) ListDevices() ([]Device, error) { return f.devices, f.err }
func (f *fakeBackend) OpenSink(SinkConfig) (Sink, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeBackend) Close() error { return nil }

func TestResolveOutputDevice(t *testing.T) {
	devices := []Device{
		{ID: "id-speakers", Name: "Speakers"},
		{ID: "id-bt", Name: "WH-1000XM5"},
		{ID: "id-dup-a", Name: "USB Audio"},
		{ID: "id-dup-b", Name: "USB Audio"},
	}

	tests := []struct {
		name    string
		savedNm string
		savedID string
		want    string
		listErr error
		devices []Device
	}{
		{name: "nothing saved -> default", want: ""},
		{name: "unique name match", savedNm: "WH-1000XM5", savedID: "stale", want: "id-bt"},
		{name: "unique name match ignores stale id", savedNm: "Speakers", savedID: "anything", want: "id-speakers"},
		{name: "duplicate name, id tiebreaker", savedNm: "USB Audio", savedID: "id-dup-b", want: "id-dup-b"},
		{name: "duplicate name, no id match -> first", savedNm: "USB Audio", savedID: "gone", want: "id-dup-a"},
		{name: "name gone, id still valid", savedNm: "Old Name", savedID: "id-speakers", want: "id-speakers"},
		{name: "name gone, id gone -> default", savedNm: "Old Name", savedID: "gone", want: ""},
		{name: "id only, present", savedID: "id-bt", want: "id-bt"},
		{name: "id only, absent -> default", savedID: "gone", want: ""},
		{name: "list error falls back to saved id", savedNm: "WH-1000XM5", savedID: "saved", want: "saved", listErr: errors.New("boom")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			devs := devices
			if tc.devices != nil {
				devs = tc.devices
			}

			b := &fakeBackend{devices: devs, err: tc.listErr}

			got := ResolveOutputDevice(b, tc.savedNm, tc.savedID)
			if got != tc.want {
				t.Fatalf("ResolveOutputDevice(%q, %q) = %q, want %q", tc.savedNm, tc.savedID, got, tc.want)
			}
		})
	}
}
