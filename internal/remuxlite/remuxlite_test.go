package remuxlite

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// testdata/small_h264_aac.mkv: 2 s, 160x90 H.264 (B-frames) at 24 fps and
// 48 kHz AAC. testdata/small_h264_ac3.mkv: 1 s with AC-3 audio. Both made
// with ffmpeg's testsrc2 and sine sources.

func TestProbe(t *testing.T) {
	if err := Probe("testdata/small_h264_aac.mkv"); err != nil {
		t.Fatalf("H.264+AAC: %v", err)
	}
	err := Probe("testdata/small_h264_ac3.mkv")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("AC-3 audio: want ErrUnsupported, got %v", err)
	}
	if err := Probe("remuxlite_test.go"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("not Matroska: want ErrUnsupported, got %v", err)
	}
}

func remux(t *testing.T, start time.Duration) *mp4.File {
	t.Helper()
	f, err := os.Open("testdata/small_h264_aac.mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out bytes.Buffer
	if err := Remux(context.Background(), f, &out, start); err != nil {
		t.Fatal(err)
	}
	mf, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("output does not parse as MP4: %v", err)
	}
	return mf
}

func countSamples(t *testing.T, mf *mp4.File) (video, audio int, firstVideoSync bool) {
	t.Helper()
	trex := map[uint32]*mp4.TrexBox{}
	for _, tx := range mf.Init.Moov.Mvex.Trexs {
		trex[tx.TrackID] = tx
	}
	first := true
	for _, seg := range mf.Segments {
		for _, frag := range seg.Fragments {
			for _, traf := range frag.Moof.Trafs {
				id := traf.Tfhd.TrackID
				for _, trun := range traf.Truns {
					for i, s := range trun.Samples {
						if id == 1 {
							if first && i == 0 {
								firstVideoSync = mp4.IsSyncSampleFlags(s.Flags)
								first = false
							}
							video++
						} else {
							audio++
						}
					}
				}
			}
		}
	}
	return video, audio, firstVideoSync
}

func TestRemux(t *testing.T) {
	mf := remux(t, 0)
	if n := len(mf.Init.Moov.Traks); n != 2 {
		t.Fatalf("want 2 tracks, got %d", n)
	}
	if typ := mf.Init.Moov.Traks[0].Mdia.Minf.Stbl.Stsd.Children[0].Type(); typ != "avc1" {
		t.Errorf("video sample entry %s, want avc1", typ)
	}
	if typ := mf.Init.Moov.Traks[1].Mdia.Minf.Stbl.Stsd.Children[0].Type(); typ != "mp4a" {
		t.Errorf("audio sample entry %s, want mp4a", typ)
	}
	v, a, sync := countSamples(t, mf)
	if v != 48 {
		t.Errorf("video samples %d, want 48", v)
	}
	if a < 90 || a > 96 { // 2 s at 48 kHz / 1024 ≈ 94
		t.Errorf("audio samples %d, want ~94", a)
	}
	if !sync {
		t.Error("first video sample is not a sync sample")
	}
}

func TestRemuxStart(t *testing.T) {
	// GOP of 24 frames: starting at 1 s skips the first GOP.
	v, _, sync := countSamples(t, remux(t, time.Second))
	if v != 24 {
		t.Errorf("video samples %d, want 24", v)
	}
	if !sync {
		t.Error("first video sample is not a sync sample")
	}
}

func TestUnlace(t *testing.T) {
	// Xiph: sizes 300 (255+45) and 2, then the rest (3 bytes).
	xiph := append([]byte{2, 255, 45, 2}, make([]byte, 305)...)
	frames, err := unlace(xiph, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 || len(frames[0]) != 300 || len(frames[1]) != 2 || len(frames[2]) != 3 {
		t.Errorf("xiph sizes wrong: %d frames", len(frames))
	}
	// Fixed: 3 frames of 4 bytes.
	frames, err = unlace(append([]byte{2}, make([]byte, 12)...), 2)
	if err != nil || len(frames) != 3 || len(frames[2]) != 4 {
		t.Errorf("fixed lacing: %v", err)
	}
	// EBML: first size 10 (0x8A), then +2 (signed 1-byte vint: 63+2 = 65 → 0xC1), rest.
	ebml := append([]byte{2, 0x8A, 0xC1}, make([]byte, 10+12+5)...)
	frames, err = unlace(ebml, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 || len(frames[0]) != 10 || len(frames[1]) != 12 || len(frames[2]) != 5 {
		t.Errorf("ebml sizes wrong: %v", []int{len(frames[0]), len(frames[1]), len(frames[2])})
	}
}
