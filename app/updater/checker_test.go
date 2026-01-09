package updater //nolint:testpackage // testing internal functions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestUpdateStatusReturnsCorrectStringRepresentation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   UpdateStatus
		expected string
	}{
		{StatusIdle, "idle"},
		{StatusChecking, "checking"},
		{StatusUpdateAvailable, "update_available"},
		{StatusDownloading, "downloading"},
		{StatusReadyToInstall, "ready_to_install"},
		{StatusInstalling, "installing"},
		{StatusError, "error"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.expected {
			t.Errorf("UpdateStatus.String() = %v, want %v", got, tt.expected)
		}
	}
}

func TestNewCheckerCreatesValidInstance(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    "https://example.com/manifest.json",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    30 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	if checker == nil {
		t.Fatal("NewChecker() returned nil")
	}

	// Extract base URL from the manifest URL (remove /manifest.json part)
	expectedBaseURL := "https://example.com"
	if checker.manifestBaseURL != expectedBaseURL {
		t.Errorf("manifestBaseURL = %v, want %v", checker.manifestBaseURL, expectedBaseURL)
	}

	if checker.channel != cfg.Channel {
		t.Errorf("channel = %v, want %v", checker.channel, cfg.Channel)
	}

	if checker.Status() != StatusIdle {
		t.Errorf("initial status = %v, want StatusIdle", checker.Status())
	}
}

func TestNewCheckerParsesDevVersionAsZero(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    "https://example.com/manifest.json",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    30 * time.Second,
		Channel:        "stable",
		CurrentVersion: "dev",
	}

	// Should not error for dev version
	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	if checker == nil {
		t.Fatal("NewChecker() returned nil")
	}

	// Dev version should parse to v0.0.0
	if checker.CurrentVersion() != "v0.0.0" {
		t.Errorf("CurrentVersion() = %v, want v0.0.0", checker.CurrentVersion())
	}
}

func TestCheckNowReturnsUpdateInfoWhenNewerVersionAvailable(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Version:     "2.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Changelog:   "- New features",
		Platforms: map[string]Platform{
			GetPlatformKey(): {
				URL:    "https://example.com/binary",
				SHA256: "abc123",
				Size:   1024,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	info, err := checker.CheckNow()
	if err != nil {
		t.Fatalf("CheckNow() error = %v", err)
	}

	if info == nil {
		t.Fatal("CheckNow() returned nil info when update should be available")
	}

	if info.AvailableVersion != "v2.0.0" {
		t.Errorf("AvailableVersion = %v, want v2.0.0", info.AvailableVersion)
	}

	if info.CurrentVersion != "v1.0.0" {
		t.Errorf("CurrentVersion = %v, want v1.0.0", info.CurrentVersion)
	}

	if checker.Status() != StatusUpdateAvailable {
		t.Errorf("Status() = %v, want StatusUpdateAvailable", checker.Status())
	}

	if checker.AvailableUpdate() == nil {
		t.Error("AvailableUpdate() returned nil after update found")
	}
}

func TestCheckNowReturnsNilWhenAlreadyOnLatestVersion(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Version:     "1.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Platforms: map[string]Platform{
			GetPlatformKey(): {
				URL:    "https://example.com/binary",
				SHA256: "abc123",
				Size:   1024,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	info, err := checker.CheckNow()
	if err != nil {
		t.Fatalf("CheckNow() error = %v", err)
	}

	if info != nil {
		t.Error("CheckNow() should return nil when already on latest")
	}

	if checker.Status() != StatusIdle {
		t.Errorf("Status() = %v, want StatusIdle", checker.Status())
	}
}

func TestCheckNowReturnsNilWhenUpdateNotApplicable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		manifestVersion string
		manifestChannel string
		checkerChannel  string
		currentVersion  string
	}{
		{
			name:            "running newer version than available",
			manifestVersion: "0.9.0",
			manifestChannel: "stable",
			checkerChannel:  "stable",
			currentVersion:  "1.0.0",
		},
		{
			name:            "channel does not match",
			manifestVersion: "2.0.0",
			manifestChannel: "beta",
			checkerChannel:  "stable",
			currentVersion:  "1.0.0",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			manifest := Manifest{
				Version:     testCase.manifestVersion,
				ReleaseDate: time.Now().UTC(),
				Channel:     testCase.manifestChannel,
				Platforms: map[string]Platform{
					GetPlatformKey(): {
						URL:    "https://example.com/binary",
						SHA256: "abc123",
						Size:   1024,
					},
				},
			}

			server := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
				resp.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(resp).Encode(manifest)
			}))
			defer server.Close()

			log := zerolog.Nop()

			cfg := CheckerConfig{
				ManifestURL:    server.URL,
				CheckInterval:  1 * time.Hour,
				HTTPTimeout:    10 * time.Second,
				Channel:        testCase.checkerChannel,
				CurrentVersion: testCase.currentVersion,
			}

			checker, err := NewChecker(cfg, log)
			if err != nil {
				t.Fatalf("NewChecker() error = %v", err)
			}

			info, err := checker.CheckNow()
			if err != nil {
				t.Fatalf("CheckNow() error = %v", err)
			}

			if info != nil {
				t.Error("CheckNow() should return nil")
			}
		})
	}
}

func TestCheckNowReturnsNilWhenNoPlatformBinaryAvailable(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Version:     "2.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Platforms: map[string]Platform{
			"unsupported-platform": {
				URL:    "https://example.com/binary",
				SHA256: "abc123",
				Size:   1024,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	info, err := checker.CheckNow()
	if err != nil {
		t.Fatalf("CheckNow() error = %v", err)
	}

	if info != nil {
		t.Error("CheckNow() should return nil when no platform binary available")
	}
}

func TestCheckNowReturnsErrorWhenServerFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	_, err = checker.CheckNow()
	if err == nil {
		t.Error("CheckNow() should return error on server error")
	}

	if checker.Status() != StatusError {
		t.Errorf("Status() = %v, want StatusError", checker.Status())
	}

	if checker.LastError() == nil {
		t.Error("LastError() should be set after error")
	}
}

func TestSetStatusUpdatesCheckerStatus(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    "https://example.com/manifest.json",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    30 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	checker.SetStatus(StatusDownloading)

	if checker.Status() != StatusDownloading {
		t.Errorf("Status() = %v, want StatusDownloading", checker.Status())
	}

	checker.SetStatus(StatusReadyToInstall)

	if checker.Status() != StatusReadyToInstall {
		t.Errorf("Status() = %v, want StatusReadyToInstall", checker.Status())
	}
}

func TestLastCheckReturnsTimeOfMostRecentCheck(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Version:     "1.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Platforms:   map[string]Platform{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	// Before first check
	if !checker.LastCheck().IsZero() {
		t.Error("LastCheck() should be zero before first check")
	}

	beforeCheck := time.Now()
	_, _ = checker.CheckNow()
	afterCheck := time.Now()

	lastCheck := checker.LastCheck()
	if lastCheck.Before(beforeCheck) || lastCheck.After(afterCheck) {
		t.Errorf("LastCheck() = %v, should be between %v and %v", lastCheck, beforeCheck, afterCheck)
	}
}

func TestCheckerStartsAndStopsWithoutPanic(t *testing.T) {
	t.Parallel()

	var checkCount int32

	server := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&checkCount, 1)

		manifest := Manifest{
			Version:     "1.0.0",
			ReleaseDate: time.Now().UTC(),
			Channel:     "stable",
			Platforms:   map[string]Platform{},
		}

		resp.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(resp).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    server.URL,
		CheckInterval:  100 * time.Millisecond, // Short interval for testing
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checker.Start(ctx)

	// Wait for initial check (10 second delay in runPeriodicCheck, so we need to wait)
	// Actually for testing purposes, let's just stop it and verify no panic
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic
	checker.Stop()
}

func TestCheckerStopsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
		manifest := Manifest{
			Version:     "1.0.0",
			ReleaseDate: time.Now().UTC(),
			Channel:     "stable",
			Platforms:   map[string]Platform{},
		}

		resp.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(resp).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    server.URL,
		CheckInterval:  100 * time.Millisecond,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	checker.Start(ctx)

	// Cancel context should stop the checker
	cancel()

	// Give it a moment to process
	time.Sleep(50 * time.Millisecond)

	// Further Stop should not hang
	checker.Stop()
}

func TestUpdateInfoStoresAllFields(t *testing.T) {
	t.Parallel()

	info := UpdateInfo{
		CurrentVersion:   "1.0.0",
		AvailableVersion: "2.0.0",
		Channel:          "stable",
		Changelog:        "- New feature",
		DownloadURL:      "https://example.com/binary",
		DownloadSize:     1024,
		SHA256:           "abc123",
		ReleaseDate:      time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	if info.CurrentVersion != "1.0.0" {
		t.Errorf("CurrentVersion = %v, want 1.0.0", info.CurrentVersion)
	}

	if info.AvailableVersion != "2.0.0" {
		t.Errorf("AvailableVersion = %v, want 2.0.0", info.AvailableVersion)
	}

	if info.DownloadSize != 1024 {
		t.Errorf("DownloadSize = %v, want 1024", info.DownloadSize)
	}
}

func TestCheckNowReturnsUpdateEvenWhenBelowMinUpgradeVersion(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Version:           "2.0.0",
		ReleaseDate:       time.Now().UTC(),
		Channel:           "stable",
		MinUpgradeVersion: "1.5.0",
		Platforms: map[string]Platform{
			GetPlatformKey(): {
				URL:    "https://example.com/binary",
				SHA256: "abc123",
				Size:   1024,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := CheckerConfig{
		ManifestURL:    server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0", // Below minimum upgrade version
	}

	checker, err := NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	// Should still return the update info (with a warning logged)
	info, err := checker.CheckNow()
	if err != nil {
		t.Fatalf("CheckNow() error = %v", err)
	}

	if info == nil {
		t.Error("CheckNow() should return update info even when below min upgrade version")
	}
}
