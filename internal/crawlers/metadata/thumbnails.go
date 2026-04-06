package metadata

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".m4v": true, ".mov": true, ".webm": true,
}

// GenerateThumbnail extracts a frame from a video using ffmpeg.
// Captures at 10% of the video duration for a representative frame.
// Skips if thumbnail already exists.
func GenerateThumbnail(videoPath string) error {
	thumbPath := thumbPathFor(videoPath)

	if _, err := os.Stat(thumbPath); err == nil {
		return nil // Already exists
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found")
	}

	// Extract a frame at ~5 minutes (or 10% for short videos)
	args := []string{
		"-ss", "300", // seek to 5 min
		"-i", videoPath,
		"-vframes", "1",
		"-q:v", "5",
		"-vf", "scale=480:-1",
		thumbPath,
	}

	cmd := exec.Command(ffmpegPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.Remove(thumbPath) // Clean up partial file
		return fmt.Errorf("ffmpeg failed: %s", string(output))
	}

	return nil
}

// GenerateThumbnailsInDir generates thumbnails for all videos in a directory.
// Returns number of thumbnails generated.
func GenerateThumbnailsInDir(dir string, recursive bool) int {
	generated := 0

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	for _, e := range entries {
		fullPath := filepath.Join(dir, e.Name())

		if e.IsDir() && recursive {
			generated += GenerateThumbnailsInDir(fullPath, true)
			continue
		}

		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !videoExts[ext] {
			continue
		}

		// Skip if thumbnail exists
		if _, err := os.Stat(thumbPathFor(fullPath)); err == nil {
			continue
		}

		fmt.Printf("    [~] Generating thumbnail: %s\n", e.Name())
		if err := GenerateThumbnail(fullPath); err != nil {
			fmt.Printf("    [!] Failed: %v\n", err)
		} else {
			fmt.Printf("    [+] Generated: %s\n", filepath.Base(thumbPathFor(fullPath)))
			generated++
		}
	}

	return generated
}

func thumbPathFor(videoPath string) string {
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	return base + "-thumb.jpg"
}
