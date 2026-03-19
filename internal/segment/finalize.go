package segment

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

func (sd *SegmentDownloader) Finalize(outputFile string) error {
	sessionDir := sd.GetSessionDir()
	tsFiles, err := filepath.Glob(filepath.Join(sessionDir, "*.ts"))
	if err != nil {
		return fmt.Errorf("failed to list segment files: %w", err)
	}

	if len(tsFiles) == 0 {
		return fmt.Errorf("no segment files found in session directory")
	}

	sort.Strings(tsFiles)

	concatFile := filepath.Join(sessionDir, "segments.txt")
	f, err := os.Create(concatFile)
	if err != nil {
		return fmt.Errorf("failed to create concat file: %w", err)
	}
	defer f.Close()

	for _, tsFile := range tsFiles {
		_, err := fmt.Fprintf(f, "file '%s'\n", tsFile)
		if err != nil {
			os.Remove(concatFile)
			return fmt.Errorf("failed to write to concat file: %w", err)
		}
	}
	f.Close()

	log.Printf("Finalizing %d segments into %s", len(tsFiles), outputFile)

	cmd := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", concatFile, "-c", "copy", "-movflags", "+faststart", outputFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	log.Printf("Successfully created %s", outputFile)

	os.Remove(concatFile)

	for _, tsFile := range tsFiles {
		if err := os.Remove(tsFile); err != nil {
			log.Printf("Warning: failed to remove %s: %v", tsFile, err)
		}
	}

	if err := os.Remove(sessionDir); err != nil {
		log.Printf("Warning: failed to remove session directory: %v", err)
	}

	return nil
}
