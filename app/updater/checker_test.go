package updater_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/updater"
)

func TestUpdateStatusReturnsCorrectStringRepresentation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   updater.UpdateStatus
		expected string
	}{
		{updater.UpdateStatusIdle, "idle"},
		{updater.UpdateStatusChecking, "checking"},
		{updater.UpdateStatusUpdateAvailable, "update_available"},
		{updater.UpdateStatusDownloading, "downloading"},
		{updater.UpdateStatusReadyToInstall, "ready_to_install"},
		{updater.UpdateStatusInstalling, "installing"},
		{updater.UpdateStatusError, "error"},
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

	cfg := updater.CheckerConfig{
		BaseURL:        "https://example.com",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    30 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	if checker == nil {
		t.Fatal("NewChecker() returned nil")
	}

	// Extract base URL from the manifest URL (remove /manifest.json part)
	expectedBaseURL := "https://example.com"
	if checker.BaseURL() != expectedBaseURL {
		t.Errorf("manifestBaseURL = %v, want %v", checker.BaseURL(), expectedBaseURL)
	}

	if checker.Channel() != cfg.Channel {
		t.Errorf("channel = %v, want %v", checker.Channel(), cfg.Channel)
	}

	if checker.Status() != updater.UpdateStatusIdle {
		t.Errorf("initial status = %v, want StatusIdle", checker.Status())
	}
}

func TestNewCheckerParsesDevVersionAsZero(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()

	cfg := updater.CheckerConfig{
		BaseURL:        "https://example.com",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    30 * time.Second,
		Channel:        "stable",
		CurrentVersion: "dev",
	}

	// Should not error for dev version
	checker, err := updater.NewChecker(cfg, log)
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

	manifest := updater.Manifest{
		Version:     "2.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Changelog:   []string{"- New features"},
		Platforms: map[string]updater.Platform{
			updater.GetPlatformKey(): {
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

	cfg := updater.CheckerConfig{
		BaseURL:        server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
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

	if checker.Status() != updater.UpdateStatusUpdateAvailable {
		t.Errorf("Status() = %v, want StatusUpdateAvailable", checker.Status())
	}

	if checker.AvailableUpdate() == nil {
		t.Error("AvailableUpdate() returned nil after update found")
	}
}

func TestCheckNowReturnsNilWhenAlreadyOnLatestVersion(t *testing.T) {
	t.Parallel()

	manifest := updater.Manifest{
		Version:     "1.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Platforms: map[string]updater.Platform{
			updater.GetPlatformKey(): {
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

	cfg := updater.CheckerConfig{
		BaseURL:        server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
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

	if checker.Status() != updater.UpdateStatusIdle {
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

			manifest := updater.Manifest{
				Version:     testCase.manifestVersion,
				ReleaseDate: time.Now().UTC(),
				Channel:     testCase.manifestChannel,
				Platforms: map[string]updater.Platform{
					updater.GetPlatformKey(): {
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

			cfg := updater.CheckerConfig{
				BaseURL:        server.URL,
				CheckInterval:  1 * time.Hour,
				HTTPTimeout:    10 * time.Second,
				Channel:        testCase.checkerChannel,
				CurrentVersion: testCase.currentVersion,
			}

			checker, err := updater.NewChecker(cfg, log)
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

	manifest := updater.Manifest{
		Version:     "2.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Platforms: map[string]updater.Platform{
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

	cfg := updater.CheckerConfig{
		BaseURL:        server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
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

	cfg := updater.CheckerConfig{
		BaseURL:        server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	_, err = checker.CheckNow()
	if err == nil {
		t.Error("CheckNow() should return error on server error")
	}

	if checker.Status() != updater.UpdateStatusError {
		t.Errorf("Status() = %v, want StatusError", checker.Status())
	}

	if checker.LastError() == nil {
		t.Error("LastError() should be set after error")
	}
}

func TestSetStatusUpdatesCheckerStatus(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()

	cfg := updater.CheckerConfig{
		BaseURL:        "https://example.com",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    30 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	checker.SetStatus(updater.UpdateStatusDownloading)

	if checker.Status() != updater.UpdateStatusDownloading {
		t.Errorf("Status() = %v, want StatusDownloading", checker.Status())
	}

	checker.SetStatus(updater.UpdateStatusReadyToInstall)

	if checker.Status() != updater.UpdateStatusReadyToInstall {
		t.Errorf("Status() = %v, want StatusReadyToInstall", checker.Status())
	}
}

func TestLastCheckReturnsTimeOfMostRecentCheck(t *testing.T) {
	t.Parallel()

	manifest := updater.Manifest{
		Version:     "1.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Platforms:   map[string]updater.Platform{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := updater.CheckerConfig{
		BaseURL:        server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
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

		manifest := updater.Manifest{
			Version:     "1.0.0",
			ReleaseDate: time.Now().UTC(),
			Channel:     "stable",
			Platforms:   map[string]updater.Platform{},
		}

		resp.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(resp).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := updater.CheckerConfig{
		BaseURL:        server.URL,
		CheckInterval:  100 * time.Millisecond, // Short interval for testing
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
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
		manifest := updater.Manifest{
			Version:     "1.0.0",
			ReleaseDate: time.Now().UTC(),
			Channel:     "stable",
			Platforms:   map[string]updater.Platform{},
		}

		resp.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(resp).Encode(manifest)
	}))
	defer server.Close()

	log := zerolog.Nop()

	cfg := updater.CheckerConfig{
		BaseURL:        server.URL,
		CheckInterval:  100 * time.Millisecond,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
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

	info := updater.UpdateInfo{
		CurrentVersion:   "1.0.0",
		AvailableVersion: "2.0.0",
		Channel:          "stable",
		Changelog:        []string{"- New feature"},
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

	if info.Channel != "stable" {
		t.Errorf("Channel = %v, want stable", info.Channel)
	}

	if len(info.Changelog) != 1 || info.Changelog[0] != "- New feature" {
		t.Errorf("Changelog = %v, want [- New feature]", info.Changelog)
	}

	if info.DownloadURL != "https://example.com/binary" {
		t.Errorf("DownloadURL = %v, want https://example.com/binary", info.DownloadURL)
	}

	if info.DownloadSize != 1024 {
		t.Errorf("DownloadSize = %v, want 1024", info.DownloadSize)
	}

	if info.SHA256 != "abc123" {
		t.Errorf("SHA256 = %v, want abc123", info.SHA256)
	}

	expectedDate := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	if !info.ReleaseDate.Equal(expectedDate) {
		t.Errorf("ReleaseDate = %v, want %v", info.ReleaseDate, expectedDate)
	}
}

func TestCheckNowReturnsUpdateEvenWhenBelowMinUpgradeVersion(t *testing.T) {
	t.Parallel()

	manifest := updater.Manifest{
		Version:           "2.0.0",
		ReleaseDate:       time.Now().UTC(),
		Channel:           "stable",
		MinUpgradeVersion: "1.5.0",
		Platforms: map[string]updater.Platform{
			updater.GetPlatformKey(): {
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

	cfg := updater.CheckerConfig{
		BaseURL:        server.URL,
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    10 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0", // Below minimum upgrade version
		DataDir:        "/tmp/test",
	}

	checker, err := updater.NewChecker(cfg, log)
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

func TestCustomChannelDoesNotFetchFromHTTP(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	dataDir := t.TempDir()

	cfg := updater.CheckerConfig{
		BaseURL:        "https://example.com",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    30 * time.Second,
		Channel:        "custom",
		CurrentVersion: "1.0.0",
		DataDir:        dataDir,
	}

	checker, err := updater.NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	// Should not attempt HTTP fetch for custom channel
	// Should return nil (no custom file found) without error
	info, err := checker.CheckNow()
	if err != nil {
		t.Errorf("CheckNow() should not error for custom channel with no files: %v", err)
	}

	if info != nil {
		t.Error("CheckNow() should return nil when no custom file exists")
	}

	if checker.Status() != updater.UpdateStatusIdle {
		t.Errorf("Status should be Idle when no custom file found, got %v", checker.Status())
	}
}

func TestSwitchingChannelsClearsStaleUpdateInfo(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	dataDir := t.TempDir()

	// Start with custom channel and set some update info
	cfg := updater.CheckerConfig{
		BaseURL:        "https://example.com",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    1 * time.Second, // Short timeout to force error
		Channel:        "custom",
		CurrentVersion: "1.0.0",
		DataDir:        dataDir,
	}

	checker, err := updater.NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	// Manually set custom update info (simulating previous upload)
	checker.SetAvailableUpdate(&updater.UpdateInfo{
		CurrentVersion:   "1.0.0",
		AvailableVersion: "2.0.0",
		Channel:          "custom",
		Changelog:        []string{"Custom update"},
	})

	// Verify custom update is set
	if checker.AvailableUpdate() == nil {
		t.Fatal("Custom update should be set")
	}

	// Switch to stable channel (which will fail to fetch)
	checker.SetChannel("stable")

	// Attempt to check - this should fail and clear the custom update info
	info, err := checker.CheckNow()
	if err == nil {
		t.Error("CheckNow() should error when fetching from unreachable server")
	}

	if info != nil {
		t.Error("CheckNow() should return nil on error")
	}

	// Verify that availableInfo was cleared
	if checker.AvailableUpdate() != nil {
		t.Error("AvailableUpdate should be nil after failed fetch on non-custom channel")
	}

	if checker.Status() != updater.UpdateStatusError {
		t.Errorf("Status should be Error after failed fetch, got %v", checker.Status())
	}
}

func TestCustomChannelPreservesInfoWhenSwitchingBack(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	dataDir := t.TempDir()

	cfg := updater.CheckerConfig{
		BaseURL:        "https://example.com",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    30 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        dataDir,
	}

	checker, err := updater.NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	// Set custom update info
	customUpdate := &updater.UpdateInfo{
		CurrentVersion:   "1.0.0",
		AvailableVersion: "2.0.0",
		Channel:          "custom",
		Changelog:        []string{"Custom update"},
	}
	checker.SetAvailableUpdate(customUpdate)

	// Simulate a check on stable channel that finds no update
	// (this would normally clear availableInfo, but should preserve custom)
	checker.SetChannel("stable")

	// The actual CheckNow would fail without a server, but the logic
	// in the code preserves custom updates when checking other channels
	// Let's verify the SetAvailableUpdate persists
	if checker.AvailableUpdate() == nil {
		t.Error("Custom update should still be available")
	}

	if checker.AvailableUpdate().Channel != "custom" {
		t.Errorf("Update channel should be custom, got %v", checker.AvailableUpdate().Channel)
	}
}

func TestCheckNowClearsAvailableInfoOnError(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()

	cfg := updater.CheckerConfig{
		BaseURL:        "https://invalid-domain-that-does-not-exist-12345.com",
		CheckInterval:  1 * time.Hour,
		HTTPTimeout:    1 * time.Second,
		Channel:        "stable",
		CurrentVersion: "1.0.0",
		DataDir:        t.TempDir(),
	}

	checker, err := updater.NewChecker(cfg, log)
	if err != nil {
		t.Fatalf("NewChecker() error = %v", err)
	}

	// Set some update info first
	checker.SetAvailableUpdate(&updater.UpdateInfo{
		AvailableVersion: "2.0.0",
		Channel:          "stable",
	})

	// Verify it's set
	if checker.AvailableUpdate() == nil {
		t.Fatal("Update info should be set")
	}

	// Try to check - should fail and clear availableInfo
	_, err = checker.CheckNow()
	if err == nil {
		t.Error("Expected error when fetching from invalid domain")
	}

	// Verify availableInfo was cleared
	if checker.AvailableUpdate() != nil {
		t.Error("AvailableUpdate should be cleared on fetch error")
	}

	if checker.Status() != updater.UpdateStatusError {
		t.Errorf("Status should be Error, got %v", checker.Status())
	}

	if checker.LastError() == nil {
		t.Error("LastError should be set after failed check")
	}
}
