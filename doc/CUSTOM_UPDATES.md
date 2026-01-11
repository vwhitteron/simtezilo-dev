# Custom Update Upload with Metadata

## Overview

When uploading custom update files via the web UI, you can include metadata to provide proper version information, changelog, and release date. This metadata will be displayed in the update panel just like manifest-based updates.

## Metadata File Format

Include a file named `manifest.json` in the root of your archive (zip or tar.gz) with the following structure:

```json
{
  "version": "1.2.3",
  "releaseDate": "2026-01-11T10:30:00Z",
  "changelog": "## Version 1.2.3\n\n### New Features\n- Feature 1\n- Feature 2\n\n### Bug Fixes\n- Fix 1\n- Fix 2",
  "platform": "darwin-arm64"
}
```

### Fields

- **version** (required): The version string (e.g., "1.2.3")
- **releaseDate** (required): ISO 8601 formatted date/time
- **changelog** (required): Markdown-formatted changelog text (supports \n for newlines)
- **platform** (optional): Target platform identifier

## Creating an Archive with Metadata

### For tar.gz archives:

```bash
# Create manifest.json
cat > manifest.json << 'EOF'
{
  "version": "1.2.3",
  "releaseDate": "2026-01-11T10:30:00Z",
  "changelog": "## What's New\n\n- Improved performance\n- Fixed bugs",
  "platform": "darwin-arm64"
}
EOF

# Create archive with metadata and binary
tar czf simtezilo-1.2.3-darwin-arm64.tar.gz manifest.json simtezilo

# Clean up
rm manifest.json
```

### For zip archives:

```bash
# Create manifest.json (same as above)
cat > manifest.json << 'EOF'
{
  "version": "1.2.3",
  "releaseDate": "2026-01-11T10:30:00Z",
  "changelog": "## What's New\n\n- Improved performance\n- Fixed bugs",
  "platform": "darwin-arm64"
}
EOF

# Create archive with metadata and binary
zip simtezilo-1.2.3-darwin-arm64.zip manifest.json simtezilo

# Clean up
rm manifest.json
```

## Upload Behavior

1. **With Metadata**: If `manifest.json` is found in the archive:
   - Version, changelog, and release date are extracted and displayed
   - The update panel shows the extracted information
   - File is saved with "custom-" prefix

2. **Without Metadata**: If no metadata file is found:
   - Falls back to synthetic version: "custom-{filename}"
   - Changelog shows: "Custom uploaded file: {filename}"
   - Release date is set to current time
   - File is saved with "custom-" prefix

## File Naming

Uploaded files are automatically prefixed with `custom-` to distinguish them from manifest-downloaded updates:
- Original: `simtezilo-1.2.3-darwin-arm64.tar.gz`
- Saved as: `custom-simtezilo-1.2.3-darwin-arm64.tar.gz`

## Cleanup

When uploading a new custom file, all previous downloads (including other custom uploads) are automatically cleaned up from the downloads directory.

## Example

See `doc/manifest.example.json` for a complete example of the metadata file.
