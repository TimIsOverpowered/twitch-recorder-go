package segment

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"twitch-recorder-go/internal/log"
)

func (sd *SegmentDownloader) finalizeInternal(outputFile string) error {
	sessionDir := sd.GetSessionDir()

	var segmentFiles []string
	var err error

	if sd.format == "mp4" {
		segmentFiles, err = filepath.Glob(filepath.Join(sessionDir, "*.mp4"))
	} else {
		segmentFiles, err = filepath.Glob(filepath.Join(sessionDir, "*.ts"))
	}

	if err != nil {
		return fmt.Errorf("failed to list segment files: %w", err)
	}

	if len(segmentFiles) == 0 {
		return fmt.Errorf("no segment files found in session directory")
	}

	sort.Strings(segmentFiles)

	concatFile := filepath.Join(sessionDir, "segments.txt")
	f, err := os.Create(concatFile)
	if err != nil {
		return fmt.Errorf("failed to create concat file: %w", err)
	}
	defer f.Close()

	for _, segFile := range segmentFiles {
		_, err := fmt.Fprintf(f, "file '%s'\n", segFile)
		if err != nil {
			os.Remove(concatFile)
			return fmt.Errorf("failed to write to concat file: %w", err)
		}
	}
	f.Close()

	log.Infof("Finalizing %d segments into %s", len(segmentFiles), outputFile)

	var cmd *exec.Cmd
	if sd.format == "mp4" {
		cmd = exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", concatFile, "-c", "copy", outputFile)
	} else {
		cmd = exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", concatFile, "-c", "copy", "-movflags", "+faststart", outputFile)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	log.Infof("Successfully created %s", outputFile)

	os.Remove(concatFile)

	for _, segFile := range segmentFiles {
		if err := os.Remove(segFile); err != nil {
			log.Warnf("Failed to remove %s: %v", segFile, err)
		}
	}

	if err := os.Remove(sessionDir); err != nil {
		log.Warnf("Failed to remove session directory: %v", err)
	}

	return nil
}
