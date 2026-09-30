package tuneassist

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cacheIdleTTL is how long a cached .gtr copy may go unused before it is deleted.
// Each use refreshes the copy's modification time, so a copy in active use stays.
const cacheIdleTTL = time.Hour

// useCached makes sure the cached copy at path exists, then arms the cache sweep.
func (s *Service) useCached(path string, write func(out io.Writer) error) error {
	err := ensureCached(path, write)
	if err != nil {
		return err
	}

	s.scheduleSweep(s.cacheTTL)

	return nil
}

// scheduleSweep runs sweepCache after delay. A later call replaces an earlier
// one, so the sweep runs one TTL after the last use.
func (s *Service) scheduleSweep(delay time.Duration) {
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()

	if s.sweepTimer == nil {
		s.sweepTimer = time.AfterFunc(delay, s.runSweep)

		return
	}

	s.sweepTimer.Reset(delay)
}

// runSweep is the timer callback. It sweeps the cache, then re-arms the timer for
// the next copy to expire, if any copy remains.
func (s *Service) runSweep() {
	next, pending := s.sweepCache()
	if pending {
		s.scheduleSweep(next)
	}
}

// sweepCache deletes every cached copy unused for longer than the TTL, and every
// temporary file left by an interrupted write. It reports how long until the next
// remaining copy expires.
//
// It holds the heavy-job slot, so no request can be reading a copy while it runs.
func (s *Service) sweepCache() (next time.Duration, pending bool) {
	dir := s.cacheDir()
	if dir == "" {
		return 0, false
	}

	release, err := s.acquire(context.Background())
	if err != nil {
		return 0, false
	}

	defer release()

	entries, err := os.ReadDir(dir)
	if err != nil {
		s.log.Debug().Err(err).Str("dir", dir).Msg("reading replay cache directory")

		return 0, false
	}

	now := time.Now()

	for _, entry := range entries {
		remaining, kept := s.expireEntry(dir, entry, now)
		if kept && (!pending || remaining < next) {
			next, pending = remaining, true
		}
	}

	return next, pending
}

// expireEntry deletes one cache entry if it is an expired copy. It reports whether
// the entry is a copy that stays, and how long until that copy expires.
func (s *Service) expireEntry(dir string, entry os.DirEntry, now time.Time) (remaining time.Duration, kept bool) {
	if entry.IsDir() || !isCachedCopy(entry.Name()) {
		return 0, false
	}

	info, err := entry.Info()
	if err != nil {
		return 0, false
	}

	remaining = s.cacheTTL - now.Sub(info.ModTime())
	if remaining > 0 {
		return remaining, true
	}

	err = os.Remove(filepath.Join(dir, entry.Name()))
	if err != nil {
		s.log.Warn().Err(err).Str("file", entry.Name()).Msg("removing expired replay cache")
	}

	return 0, false
}

// isCachedCopy reports whether name is a cached .gtr copy, or a temporary file that
// ensureCached writes before it renames the file into place.
func isCachedCopy(name string) bool {
	return strings.HasSuffix(name, ".gtr") || strings.Contains(name, ".gtr.")
}
