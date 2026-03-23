package segment

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"twitch-recorder-go/internal/log"
)

const (
	MaxSegmentSize = 50 * 1024 * 1024 // 50 MB per segment
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

	totalSize := int64(0)
	for _, seg := range segmentFiles {
		info, err := os.Stat(seg)
		if err != nil {
			return fmt.Errorf("failed to stat segment %s: %w", seg, err)
		}

		if info.Size() > MaxSegmentSize {
			return fmt.Errorf("segment %s exceeds maximum size (%d bytes)", seg, MaxSegmentSize)
		}

		totalSize += info.Size()
	}

	sort.Slice(segmentFiles, func(i, j int) bool {
		nameI := filepath.Base(segmentFiles[i])
		nameJ := filepath.Base(segmentFiles[j])
		if nameI == "init.mp4" {
			return true
		}
		if nameJ == "init.mp4" {
			return false
		}
		numI, _ := strconv.Atoi(strings.TrimSuffix(nameI, filepath.Ext(nameI)))
		numJ, _ := strconv.Atoi(strings.TrimSuffix(nameJ, filepath.Ext(nameJ)))
		return numI < numJ
	})

	log.InfofC(sd.channel, "Finalizing %d segments into %s", len(segmentFiles), outputFile)

	var cmd *exec.Cmd
	if sd.format == "mp4" {
		cmd = exec.Command("ffmpeg",
			"-y",
			"-i", "pipe:0",
			"-c", "copy",
			"-avoid_negative_ts", "make_zero",
			"-fflags", "+genpts",
			"-movflags", "+faststart",
			outputFile,
		)

		pr, pw := io.Pipe()
		cmd.Stdin = pr

		go func() {
			defer pw.Close()
			for _, segFile := range segmentFiles {
				f, err := os.Open(segFile)
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				io.Copy(pw, f)
				f.Close()
			}
		}()
	} else {
		concatFile := filepath.Join(sessionDir, "segments.txt")
		f, err := os.Create(concatFile)
		if err != nil {
			return fmt.Errorf("failed to create concat file: %w", err)
		}
		for _, segFile := range segmentFiles {
			if _, err := fmt.Fprintf(f, "file '%s'\n", segFile); err != nil {
				f.Close()
				os.Remove(concatFile)
				return fmt.Errorf("failed to write to concat file: %w", err)
			}
		}
		f.Close()

		cmd = exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", concatFile, "-c", "copy", "-output_ts_offset", "0", "-movflags", "+faststart", outputFile)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		log.ErrorfC(sd.channel, "FFmpeg stderr: %s", stderrBuf.String())
		return fmt.Errorf("ffmpeg failed: %w (output: %s)", err, strings.TrimSpace(stderrBuf.String()))
	}

	if sd.format != "mp4" {
		concatFile := filepath.Join(sessionDir, "segments.txt")
		os.Remove(concatFile)
	}

	if len(stdoutBuf.Bytes()) > 0 {
		log.DebugfC(sd.channel, "FFmpeg output: %s", stdoutBuf.String())
	}

	log.InfofC(sd.channel, "Successfully created %s", outputFile)

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
