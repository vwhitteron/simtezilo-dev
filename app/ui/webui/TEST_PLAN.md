# Web UI Test Plan - Update Manager

This document outlines the test scenarios for the Update Manager functionality in the web UI. These tests should be performed manually until a JavaScript testing framework is implemented.

## Test Scenarios

### 1. Channel Switching Behavior

#### Test: Custom Channel Triggers Status Check
**Steps:**
1. Start with any non-custom channel (stable, beta, dev)
2. Switch to custom channel
3. Observe network requests

**Expected:**
- A status fetch request should be made to `/api/updates/status`
- The UI should show "checking" state briefly
- If a custom file exists, its metadata should be displayed

**Actual Backend Behavior:**
- `CheckNow()` is called
- `checkCustomUpdate()` scans for files starting with "custom-"
- Manifest is extracted from the archive

#### Test: Non-Custom Channel Clears Stale Data
**Steps:**
1. Upload a custom update file (with channel = "custom")
2. Switch to custom channel and verify metadata is displayed
3. Switch to stable channel (with no server available)

**Expected:**
- Custom update info should NOT appear on stable channel
- Error message should be shown indicating connection failure
- Status should show "error" state

**Backend Behavior:**
- Fetch fails with timeout/connection error
- `availableInfo` is cleared (`= nil`)
- `lastError` is set with the error

#### Test: Custom Channel Preserves Info During Switching
**Steps:**
1. Upload custom update and switch to custom channel
2. Switch to stable channel (successfully fetches, but no update available)
3. Switch back to custom channel

**Expected:**
- Custom update info should still be available
- Status should show "ready to install" for custom channel

**Backend Behavior:**
- Code checks `if c.availableInfo.Channel != "custom"` before clearing
- Custom updates are preserved across channel switches

---

### 2. Changelog Scroll Behavior

#### Test: Overscroll Containment
**Steps:**
1. Have a custom update with long changelog (>200px height)
2. Scroll within the changelog box to the top
3. Continue scrolling upward

**Expected:**
- Page should NOT scroll when at the top of changelog
- Scroll should be contained within the changelog box

**Steps:**
1. Scroll to the bottom of changelog
2. Continue scrolling downward

**Expected:**
- Page should NOT scroll when at the bottom of changelog
- Scroll should remain contained

**Implementation:**
- Uses CSS `overscroll-behavior: contain;` on the changelog container

---

### 3. Custom File Detection

#### Test: Custom File Naming
**Steps:**
1. Place a file NOT starting with "custom-" in downloads directory
2. Switch to custom channel

**Expected:**
- No update should be detected
- Status should be "idle"

**Steps:**
1. Place a file starting with "custom-" in downloads directory
2. Switch to custom channel

**Expected:**
- File should be detected
- Manifest should be extracted
- Update info should be displayed

#### Test: Manifest Extraction
**Steps:**
1. Create a tar.gz file with `manifest.json` in root
2. Name it `custom-test.tar.gz` and place in downloads
3. Switch to custom channel

**Expected:**
- Manifest is successfully extracted
- Version, changelog, release date are displayed

**Steps:**
1. Repeat with a zip file containing `manifest.json`

**Expected:**
- Same behavior as tar.gz

---

### 4. Error Handling

#### Test: Network Error Display
**Setup:** Ensure no update server is available

**Steps:**
1. Switch to stable channel
2. Wait for check to complete

**Expected:**
- Error state should be displayed
- Connection error message should be shown
- No stale update info from other channels

#### Test: Invalid Manifest Handling
**Steps:**
1. Create custom-test.tar.gz with invalid JSON in manifest.json
2. Switch to custom channel

**Expected:**
- Error should be logged
- Status should show "error"
- Appropriate error message displayed

---

## Automation Recommendations

To automate these tests, consider implementing:

### 1. Backend Integration Tests
Currently implemented in `checker_test.go`:
- `TestCustomChannelDoesNotFetchFromHTTP` ✓
- `TestSwitchingChannelsClearsStaleUpdateInfo` ✓
- `TestCustomChannelPreservesInfoWhenSwitchingBack` ✓
- `TestCheckNowClearsAvailableInfoOnError` ✓

### 2. Frontend Test Framework (Future)
Consider adding:
- **Jest** or **Vitest** for unit testing JavaScript functions
- **Playwright** or **Cypress** for E2E testing of UI interactions
- Mock API responses for testing different scenarios

### Example Test Structure (if Jest were added):

```javascript
describe('UpdateManager', () => {
  describe('channel switching', () => {
    it('should fetch status when switching to custom channel', async () => {
      // Setup mock fetch
      global.fetch = jest.fn(() => 
        Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ status: 'idle' })
        })
      );
      
      // Simulate channel change to custom
      UpdateManager.elements.channel.value = 'custom';
      UpdateManager.elements.channel.dispatchEvent(new Event('change'));
      
      // Wait for async operations
      await new Promise(resolve => setTimeout(resolve, 600));
      
      // Assert fetch was called
      expect(fetch).toHaveBeenCalledWith('/api/updates/status');
    });
    
    it('should clear stale data when switching to failed channel', async () => {
      // Setup initial custom update
      UpdateManager.state.updateAvailable = true;
      UpdateManager.state.channel = 'custom';
      
      // Mock failed fetch
      global.fetch = jest.fn(() => 
        Promise.reject(new Error('Network error'))
      );
      
      // Switch to stable
      UpdateManager.elements.channel.value = 'stable';
      UpdateManager.elements.channel.dispatchEvent(new Event('change'));
      
      await new Promise(resolve => setTimeout(resolve, 600));
      
      // Assert state was cleared
      expect(UpdateManager.state.updateAvailable).toBe(false);
    });
  });
  
  describe('changelog scroll', () => {
    it('should not propagate scroll to parent at boundaries', () => {
      const changelog = document.querySelector('.card-body');
      const style = window.getComputedStyle(changelog);
      
      expect(style.overscrollBehavior).toBe('contain');
    });
  });
});
```

---

## Manual Testing Checklist

When making changes to the Update Manager, verify:

- [ ] Custom channel detects files starting with "custom-"
- [ ] Switching to custom channel triggers status check
- [ ] Non-custom channels show errors when server unavailable
- [ ] Stale custom data doesn't appear on non-custom channels
- [ ] Changelog scroll doesn't affect page scroll at boundaries
- [ ] Manifest extraction works for both .tar.gz and .zip
- [ ] Error states are displayed appropriately
- [ ] Upload functionality still works as expected
