package audio_test

import (
	"errors"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

// fakeBackend is a Backend whose only meaningful behaviour is ListDevices,
// which ResolveOutputDevice depends on.
type fakeBackend struct {
	devices []audio.Device
	err     error
}

func (f *fakeBackend) Name() string                         { return "fake" }
func (f *fakeBackend) ListDevices() ([]audio.Device, error) { return f.devices, f.err }
func (f *fakeBackend) OpenSink(audio.SinkConfig) (audio.Sink, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeBackend) Close() error { return nil }

func TestResolveOutputDevice(t *testing.T) {
	t.Parallel()

	devices := []audio.Device{
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
		devices []audio.Device
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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			devs := devices
			if testCase.devices != nil {
				devs = testCase.devices
			}

			b := &fakeBackend{devices: devs, err: testCase.listErr}

			got := audio.ResolveOutputDevice(b, testCase.savedNm, testCase.savedID)
			if got != testCase.want {
				t.Fatalf("ResolveOutputDevice(%q, %q) = %q, want %q", testCase.savedNm, testCase.savedID, got, testCase.want)
			}
		})
	}
}
