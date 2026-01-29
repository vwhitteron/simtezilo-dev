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
func (p *manager) extractArchive(archivePath, destDir string) error {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz"):
		return p.extractTarGz(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".zip"):
		return p.extractZip(archivePath, destDir)
	default:
		// Treat as raw binary
		destBinDir := filepath.Join(destDir, "Simtezilo", "bin")

		err := os.MkdirAll(destBinDir, 0o755)
		if err != nil {
			return err
		}

		return p.copyFile(archivePath, filepath.Join(destBinDir, updateBinaryName))
	}
}

// extractTarGz extracts a gzip-compressed tar archive to the destination directory,
// preserving file permissions for executable files.
func (p *manager) extractTarGz(archivePath, destDir string) error {
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

		err = p.extractTarEntry(header, tarReader, destDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractTarEntry processes a single entry from a tar archive, creating directories
// or extracting files as appropriate with path traversal protection.
func (p *manager) extractTarEntry(header *tar.Header, tarReader *tar.Reader, destDir string) error {
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
		err := p.extractTarFile(header, tarReader, target)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractTarFile extracts a regular file from a tar archive to the target path,
// creating parent directories as needed and preserving executable permissions.
func (p *manager) extractTarFile(header *tar.Header, tarReader *tar.Reader, target string) error {
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
			p.log.Warn().Err(err).Str("file", target).Msg("Failed to set executable permissions")
		}
	}

	return nil
}

// extractZip extracts a zip archive to the destination directory, preserving
// file permissions for executable files.
func (p *manager) extractZip(archivePath, destDir string) error {
	zipReader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer zipReader.Close()

	for _, zipFile := range zipReader.File {
		err = p.extractZipEntry(zipFile, destDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractZipEntry processes a single entry from a zip archive, creating directories
// or extracting files as appropriate with path traversal protection.
func (p *manager) extractZipEntry(zipFile *zip.File, destDir string) error {
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

	return p.extractZipFile(zipFile, target)
}

// extractZipFile extracts a regular file from a zip archive to the target path,
// creating parent directories as needed and preserving executable permissions.
func (p *manager) extractZipFile(zipFile *zip.File, target string) error {
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
			p.log.Warn().Err(err).Str("file", target).Msg("Failed to set executable permissions")
		}
	}

	return nil
}

// installInitScripts copies init scripts from the extracted archive to the
// system init directory, setting executable permissions on each file.
func (p *manager) installInitScripts(sourceDir string) error {
	return p.installFiles(sourceDir, p.initDir(), 0o755, "init scripts")
}

// installConfigFiles copies configuration files from the extracted archive to the
// system etc directory, preserving existing configuration unless explicitly updated.
func (p *manager) installConfigFiles(sourceDir string) error {
	return p.installFiles(sourceDir, p.etcDir(), 0o644, "config files")
}

// installFiles copies files from source directory to destination directory with the specified permissions.
func (p *manager) installFiles(sourceDir, destDir string, fileMode os.FileMode, description string) error {
	_, err := os.Stat(sourceDir)
	if err != nil {
		p.log.Debug().Str("type", description).Msg("No files to install")

		return nil //nolint:nilerr // intentionally return nil when source dir doesn't exist
	}

	p.log.Info().Str("from", sourceDir).Str("to", destDir).Msgf("Installing %s", description)

	err = os.MkdirAll(destDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
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
		dstPath := filepath.Join(destDir, entry.Name())

		p.log.Debug().Str("file", entry.Name()).Msgf("Installing %s", description)

		err := p.copyFile(srcPath, dstPath)
		if err != nil {
			return fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
		}

		err = os.Chmod(dstPath, fileMode)
		if err != nil {
			p.log.Warn().Err(err).Str("file", dstPath).Msg("Failed to set permissions")
		}
	}

	return nil
}

// installAdditionalBinaries copies any additional binaries from the extracted
// archive to the install directory, skipping the main binary which is handled separately.
func (p *manager) installAdditionalBinaries(sourceDir, destDir, mainBinary string) error {
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

		p.log.Debug().Str("binary", name).Msg("Installing additional binary")

		err := p.copyFile(srcPath, dstPath)
		if err != nil {
			return fmt.Errorf("failed to copy %s: %w", name, err)
		}

		err = os.Chmod(dstPath, 0o755)
		if err != nil {
			p.log.Warn().Err(err).Str("file", dstPath).Msg("Failed to set permissions")
		}
	}

	return nil
}

// copyFile copies a file from src to dst, creating the destination file if it
// doesn't exist or truncating it if it does.
func (p *manager) copyFile(src, dst string) error {
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

// createRollbackArchive creates a tgz archive containing the current versions
// of critical system files that can be restored during a rollback operation.
// Files included: bin/simtezilo, bin/platform, init/simtezilo.service,
// init/recover.sh, and etc/simtezilo.conf.
func (p *manager) createRollbackArchive() error {
	p.log.Info().Str("archive", p.rollbackArchive()).Msg("Creating rollback archive")

	// Define files to backup with their source paths and archive paths
	filesToBackup := []struct {
		sourcePath  string
		archivePath string
	}{
		{filepath.Join(p.installDir(), "simtezilo"), "bin/simtezilo"},
		{filepath.Join(p.installDir(), "platform"), "bin/platform"},
		{filepath.Join(p.initDir(), "simtezilo.service"), "init/simtezilo.service"},
		{filepath.Join(p.initDir(), "recover.sh"), "init/recover.sh"},
		{filepath.Join(p.etcDir(), "simtezilo.conf"), "etc/simtezilo.conf"},
	}

	// Create archive file
	archiveFile, err := os.Create(p.rollbackArchive())
	if err != nil {
		return fmt.Errorf("failed to create archive file: %w", err)
	}
	defer archiveFile.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Add each file to the archive
	for _, file := range filesToBackup {
		err := p.addFileToArchive(tarWriter, file.sourcePath, file.archivePath)
		if err != nil {
			p.log.Warn().
				Err(err).
				Str("file", file.sourcePath).
				Msg("Failed to add file to rollback archive")
			// Continue adding other files even if one fails
		}
	}

	p.log.Info().Msg("Rollback archive created successfully")

	return nil
}

// addFileToArchive adds a single file to a tar archive with the specified path.
func (p *manager) addFileToArchive(tarWriter *tar.Writer, sourcePath, archivePath string) error {
	// Check if file exists
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			p.log.Debug().Str("file", sourcePath).Msg("File does not exist, skipping")

			return nil
		}

		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Skip directories
	if fileInfo.IsDir() {
		return nil
	}

	// Open source file
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create tar header
	header := &tar.Header{
		Name:    archivePath,
		Size:    fileInfo.Size(),
		Mode:    int64(fileInfo.Mode()),
		ModTime: fileInfo.ModTime(),
	}

	// Write header
	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	// Write file contents
	_, err = io.Copy(tarWriter, file)
	if err != nil {
		return fmt.Errorf("failed to write file to archive: %w", err)
	}

	p.log.Debug().Str("file", archivePath).Msg("Added file to rollback archive")

	return nil
}
