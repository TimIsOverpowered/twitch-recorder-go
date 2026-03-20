package segment

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"twitch-recorder-go/internal/log"
)

func (sd *SegmentDownloader) finalizeInternal(outputFile string) error {
	sessionDir := sd.GetSessionDir()
	channelDir := sd.GetChannelDir()

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

	sort.Slice(segmentFiles, func(i, j int) bool {
		nameI := filepath.Base(segmentFiles[i])
		nameJ := filepath.Base(segmentFiles[j])
		numI := strings.TrimSuffix(nameI, filepath.Ext(nameI))
		numJ := strings.TrimSuffix(nameJ, filepath.Ext(nameJ))
		idxI, _ := strconv.Atoi(numI)
		idxJ, _ := strconv.Atoi(numJ)
		return idxI < idxJ
	})

	var filteredSegments []string
	for _, segFile := range segmentFiles {
		if filepath.Base(segFile) != "init.mp4" {
			filteredSegments = append(filteredSegments, segFile)
		}
	}
	segmentFiles = filteredSegments

	if sd.format == "mp4" && sd.initSegment != "" {
		log.InfofC(sd.channel, "Prepending init segment to %d media segments", len(segmentFiles))

		initPath := filepath.Join(sessionDir, sd.initSegment)
		initSavePath := filepath.Join(sessionDir, "init.mp4")

		if err := os.Rename(initPath, initSavePath); err != nil {
			log.WarnfC(sd.channel, "Failed to rename init segment: %v", err)
		} else {
			log.DebugfC(sd.channel, "Saved init segment as init.mp4")
			initFile := filepath.Join(sessionDir, "init.mp4")
			segmentFiles = append([]string{initFile}, segmentFiles...)
		}
	}

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

	log.InfofC(sd.channel, "Finalizing %d segments into %s", len(segmentFiles), outputFile)

	var cmd *exec.Cmd
	if sd.format == "mp4" {
		concatArg := "concat:" + strings.Join(segmentFiles, "|")
		cmd = exec.Command("ffmpeg", "-y", "-i", concatArg, "-c", "copy", "-avoid_negative_ts", "make_zero", "-fflags", "+genpts", "-movflags", "+faststart", outputFile)
	} else {
		cmd = exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", concatFile, "-c", "copy", "-output_ts_offset", "0", "-movflags", "+faststart", outputFile)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	log.InfofC(sd.channel, "Successfully created %s", outputFile)

	os.Remove(concatFile)

	for _, segFile := range segmentFiles {
		if err := os.Remove(segFile); err != nil {
			log.WarnfC(sd.channel, "Failed to remove %s: %v", segFile, err)
		}
	}

	if err := sd.DeleteSessionMetadata(); err != nil {
		log.WarnfC(sd.channel, "Failed to delete session metadata: %v", err)
	}

	sessionDirName := filepath.Base(sessionDir)
	sessionDirParent := filepath.Dir(sessionDir)
	folderName := strings.TrimSuffix(filepath.Base(outputFile), ".mp4")
	renameTarget := filepath.Join(sessionDirParent, folderName)

	if err := os.Rename(sessionDir, renameTarget); err != nil {
		log.WarnfC(sd.channel, "Failed to rename session directory %s to %s: %v", sessionDirName, folderName, err)
	} else {
		log.InfofC(sd.channel, "Renamed session directory from %s to %s", sessionDirName, folderName)
	}

	if _, err := os.Stat(sessionDirParent); os.IsNotExist(err) {
		if err := os.Remove(channelDir); err != nil && !os.IsNotExist(err) {
			log.WarnfC(sd.channel, "Failed to remove empty channel directory: %v", err)
		}
	}

	return nil
}
