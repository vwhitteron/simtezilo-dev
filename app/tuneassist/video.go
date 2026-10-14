package tuneassist

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// videoExt is the container extension for a replay video carrying an embedded
// telemetry track. Only MP4 is supported: the telemetry track is written as an
// ISO base media timed metadata track, which has no equivalent in other formats.
const videoExt = ".mp4"

// isVideoName reports whether a replay listing entry is a video rather than a
// plain telemetry recording.
func isVideoName(name string) bool {
	return strings.EqualFold(filepath.Ext(name), videoExt)
}

// HandleVideo serves a replay video for the tuning assistant's video panel.
//
// The response goes through http.ServeContent, which supplies range requests,
// Last-Modified and conditional GETs. The browser needs all three: it seeks the
// video constantly while the user scrubs, and a replay video runs to hundreds of
// megabytes.
func (s *Service) HandleVideo(response http.ResponseWriter, request *http.Request) {
	dir := s.replayDir()
	replays := s.listReplays(dir)

	filename := request.URL.Query().Get("replay")

	status, valid := validateReplayName(filename, replays)
	if !valid {
		http.Error(response, "invalid or unknown replay filename", status)

		return
	}

	if !isVideoName(filename) {
		http.Error(response, "replay is not a video", http.StatusBadRequest)

		return
	}

	file, err := os.Open(filepath.Join(dir, filename))
	if err != nil {
		s.log.Error().Err(err).Str("replay", filename).Msg("opening replay video")
		http.Error(response, "opening replay video", http.StatusInternalServerError)

		return
	}

	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		s.log.Error().Err(err).Str("replay", filename).Msg("stating replay video")
		http.Error(response, "stating replay video", http.StatusInternalServerError)

		return
	}

	// The shared server sets a 30 second write timeout, which a video body will not
	// finish inside. Clear the deadline for this response alone rather than relaxing
	// it for every route. A wrapped ResponseWriter would make this fail, so treat the
	// error as advisory: the transfer still works, it is just capped again.
	controller := http.NewResponseController(response)

	err = controller.SetWriteDeadline(time.Time{})
	if err != nil {
		s.log.Debug().Err(err).Msg("clearing write deadline for video response")
	}

	response.Header().Set("Content-Type", "video/mp4")

	http.ServeContent(response, request, filename, info.ModTime(), file)
}
