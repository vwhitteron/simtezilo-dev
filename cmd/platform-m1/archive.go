package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractArchive extracts an archive file to the destination directory, automatically
// detecting the format based on file extension (.tar.gz, .tgz, .zip, or raw binary).
func (m *manager) extractArchive(archivePath, destDir string) error {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz"):
		return m.extractTarGz(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".zip"):
		return m.extractZip(archivePath, destDir)
	default:
		// Treat as raw binary
		destBinDir := filepath.Join(destDir, "Simtezilo", "bin")

		err := os.MkdirAll(destBinDir, 0o755)
		if err != nil {
			return err
		}

		return m.copyFile(archivePath, filepath.Join(destBinDir, updateBinaryName))
	}
}

// extractTarGz extracts a gzip-compressed tar archive to the destination directory,
// preserving file permissions for executable files.
func (m *manager) extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tarReader := tar.NewReader(gzr)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		err = m.extractTarEntry(header, tarReader, destDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractTarEntry processes a single entry from a tar archive, creating directories
// or extracting files as appropriate with path traversal protection.
func (m *manager) extractTarEntry(header *tar.Header, tarReader *tar.Reader, destDir string) error {
	target := filepath.Join(destDir, header.Name) //nolint:gosec // controlled input

	if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
		return fmt.Errorf("invalid path in archive: %s", header.Name)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		err := os.MkdirAll(target, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	case tar.TypeReg:
		err := m.extractTarFile(header, tarReader, target)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractTarFile extracts a regular file from a tar archive to the target path,
// creating parent directories as needed and preserving executable permissions.
func (m *manager) extractTarFile(header *tar.Header, tarReader *tar.Reader, target string) error {
	err := os.MkdirAll(filepath.Dir(target), 0o755)
	if err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	outFile, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	_, err = io.Copy(outFile, tarReader)
	if err != nil {
		outFile.Close()

		return fmt.Errorf("failed to write file: %w", err)
	}

	outFile.Close()

	if header.Mode&0o111 != 0 {
		err = os.Chmod(target, 0o755)
		if err != nil {
			m.log.Warn().Err(err).Str("file", target).Msg("Failed to set executable permissions")
		}
	}

	return nil
}

// extractZip extracts a zip archive to the destination directory, preserving
// file permissions for executable files.
func (m *manager) extractZip(archivePath, destDir string) error {
	zipReader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer zipReader.Close()

	for _, zipFile := range zipReader.File {
		err = m.extractZipEntry(zipFile, destDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractZipEntry processes a single entry from a zip archive, creating directories
// or extracting files as appropriate with path traversal protection.
func (m *manager) extractZipEntry(zipFile *zip.File, destDir string) error {
	target := filepath.Join(destDir, zipFile.Name) //nolint:gosec // controlled input

	if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
		return fmt.Errorf("invalid path in archive: %s", zipFile.Name)
	}

	if zipFile.FileInfo().IsDir() {
		err := os.MkdirAll(target, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		return nil
	}

	return m.extractZipFile(zipFile, target)
}

// extractZipFile extracts a regular file from a zip archive to the target path,
// creating parent directories as needed and preserving executable permissions.
func (m *manager) extractZipFile(zipFile *zip.File, target string) error {
	err := os.MkdirAll(filepath.Dir(target), 0o755)
	if err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	fileReader, err := zipFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer fileReader.Close()

	outFile, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, fileReader) //nolint:gosec // controlled input
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if zipFile.Mode()&0o111 != 0 {
		err = os.Chmod(target, 0o755)
		if err != nil {
			m.log.Warn().Err(err).Str("file", target).Msg("Failed to set executable permissions")
		}
	}

	return nil
}

// installInitScripts copies init scripts from the extracted archive to the
// system init directory, setting executable permissions on each file.
func (m *manager) installInitScripts(sourceDir string) error {
	_, err := os.Stat(sourceDir)
	if err != nil {
		m.log.Debug().Msg("No init scripts to install")

		return nil //nolint:nilerr // intentionally return nil when source dir doesn't exist
	}

	m.log.Info().Str("from", sourceDir).Str("to", updateInitDir).Msg("Installing init scripts")

	err = os.MkdirAll(updateInitDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create init directory: %w", err)
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(sourceDir, entry.Name())
		dstPath := filepath.Join(updateInitDir, entry.Name())

		m.log.Debug().Str("script", entry.Name()).Msg("Installing init script")

		err := m.copyFile(srcPath, dstPath)
		if err != nil {
			return fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
		}

		err = os.Chmod(dstPath, 0o755)
		if err != nil {
			m.log.Warn().Err(err).Str("file", dstPath).Msg("Failed to set permissions")
		}
	}

	return nil
}

// installAdditionalBinaries copies any additional binaries from the extracted
// archive to the install directory, skipping the main binary which is handled separately.
func (m *manager) installAdditionalBinaries(sourceDir, destDir, mainBinary string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == mainBinary {
			continue // Already installed
		}

		srcPath := filepath.Join(sourceDir, name)
		dstPath := filepath.Join(destDir, name)

		m.log.Debug().Str("binary", name).Msg("Installing additional binary")

		err := m.copyFile(srcPath, dstPath)
		if err != nil {
			return fmt.Errorf("failed to copy %s: %w", name, err)
		}

		err = os.Chmod(dstPath, 0o755)
		if err != nil {
			m.log.Warn().Err(err).Str("file", dstPath).Msg("Failed to set permissions")
		}
	}

	return nil
}

// copyFile copies a file from src to dst, creating the destination file if it
// doesn't exist or truncating it if it does.
func (m *manager) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	return nil
}
