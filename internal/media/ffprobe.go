package media

import (
	"encoding/json"
	"os/exec"
	"strconv"
)

// ProbeDuration runs ffprobe to get the duration of a video file in seconds.
// Returns 0 if ffprobe is unavailable or fails.
func ProbeDuration(videoPath string) float64 {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}

	cmd := exec.Command(ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_entries", "format=duration",
		"-i", videoPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return 0
	}

	seconds, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil {
		return 0
	}
	return seconds
}
