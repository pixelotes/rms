package subtitles

import (
	"encoding/binary"
	"fmt"
	"os"
)

// computeMovieHash computes the OSDb hash for OpenSubtitles.
// Reads first+last 64KB and adds file size.
func computeMovieHash(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return "", err
	}
	fileSize := stat.Size()

	const chunkSize = 65536
	if fileSize < chunkSize {
		return "", fmt.Errorf("file too small to hash")
	}

	buf := make([]byte, 8)
	var hash uint64 = uint64(fileSize)

	// Read first 64KB
	for i := 0; i < chunkSize/8; i++ {
		if _, err := file.Read(buf); err != nil {
			return "", err
		}
		hash += binary.LittleEndian.Uint64(buf)
	}

	// Read last 64KB
	if _, err := file.Seek(fileSize-chunkSize, 0); err != nil {
		return "", err
	}
	for i := 0; i < chunkSize/8; i++ {
		if _, err := file.Read(buf); err != nil {
			return "", err
		}
		hash += binary.LittleEndian.Uint64(buf)
	}

	return fmt.Sprintf("%x", hash), nil
}
