package remuxlite

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Eyevinn/mp4ff/aac"
	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/hevc"
	"github.com/Eyevinn/mp4ff/mp4"
)

// ErrUnsupported wraps the reason a file cannot be remuxed without ffmpeg.
var ErrUnsupported = errors.New("not supported by remux-lite")

const (
	videoTimescale = 90000
	// maxFragment flushes a fragment early when a GOP is unusually large.
	maxFragment = 16 << 20
)

// Probe reports whether a file can be remuxed: a Matroska container with
// H.264/HEVC video and AAC audio (or no audio). It only reads the headers.
func Probe(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d, err := openDemuxer(f)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	_, _, err = selectTracks(d)
	return err
}

func selectTracks(d *demuxer) (video, audio *track, err error) {
	var otherAudio []string
	for _, t := range d.tracks {
		if !t.enabled {
			continue
		}
		switch t.typ {
		case 1:
			if video == nil {
				video = t
			}
		case 2:
			if !strings.HasPrefix(t.codec, "A_AAC") || t.encoded {
				otherAudio = append(otherAudio, t.codec)
				continue
			}
			if audio == nil || (t.isDefault && !audio.isDefault) {
				audio = t
			}
		}
	}
	if video == nil {
		return nil, nil, fmt.Errorf("%w: no video track", ErrUnsupported)
	}
	switch video.codec {
	case "V_MPEG4/ISO/AVC", "V_MPEGH/ISO/HEVC":
	default:
		return nil, nil, fmt.Errorf("%w: video codec %s", ErrUnsupported, video.codec)
	}
	if video.encoded {
		return nil, nil, fmt.Errorf("%w: video track uses content encoding", ErrUnsupported)
	}
	if len(video.private) < 23 {
		return nil, nil, fmt.Errorf("%w: video track has no decoder configuration", ErrUnsupported)
	}
	if audio == nil && len(otherAudio) > 0 {
		return nil, nil, fmt.Errorf("%w: audio codec %s", ErrUnsupported, strings.Join(otherAudio, ", "))
	}
	return video, audio, nil
}

// Remux reads a Matroska stream and writes fragmented MP4 to w, starting at
// the first video keyframe at or after start. Timestamps start at zero.
func Remux(ctx context.Context, src io.Reader, w io.Writer, start time.Duration) error {
	d, err := openDemuxer(src)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	vt, at, err := selectTracks(d)
	if err != nil {
		return err
	}
	m := &muxer{d: d, w: w, video: vt, audio: at}
	if err := m.writeInit(); err != nil {
		return err
	}
	startTicks := int64(start) / d.scale
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := d.next()
		if err == io.EOF {
			return m.flush(true)
		}
		if err != nil {
			return err
		}
		if err := m.add(b, startTicks); err != nil {
			return err
		}
	}
}

type sample struct {
	pts  int64 // ticks
	key  bool
	data []byte
}

type muxer struct {
	d            *demuxer
	w            io.Writer
	video, audio *track
	vID, aID     uint32
	nalLen       int
	audioRate    int
	audioFrame   int64 // ticks per AAC frame, for laced blocks
	started      bool
	base         int64 // ticks of the first emitted keyframe
	vbuf, abuf   []sample
	bufBytes     int
	seq          uint32
	lastVDTS     int64 // in videoTimescale; -1 before the first sample
	lastVDur     int64
	lastADT      int64 // in audio timescale
	lastADur     int64
	// delay shifts every timestamp so composition offsets are not negative
	// (decode times are sorted presentation times, which run ahead of the
	// first B-frames). Set from the first GOP.
	delay    int64 // in videoTimescale
	delaySet bool
	pending  []sample // audio read before the first keyframe
}

func (m *muxer) writeInit() error {
	init := mp4.CreateEmptyInit()
	brands := []string{"isom", "iso6", "mp41"}
	vt := init.AddEmptyTrack(videoTimescale, "video", "und")
	m.vID = vt.Tkhd.TrackID
	switch m.video.codec {
	case "V_MPEG4/ISO/AVC":
		m.nalLen = int(m.video.private[4]&3) + 1
		rec, err := avc.DecodeAVCDecConfRec(m.video.private)
		if err != nil {
			return fmt.Errorf("%w: avcC: %v", ErrUnsupported, err)
		}
		if err := vt.SetAVCDescriptor("avc1", rec.SPSnalus, rec.PPSnalus, true); err != nil {
			return fmt.Errorf("%w: avcC: %v", ErrUnsupported, err)
		}
		brands = append(brands, "avc1")
	case "V_MPEGH/ISO/HEVC":
		m.nalLen = int(m.video.private[21]&3) + 1
		rec, err := hevc.DecodeHEVCDecConfRec(m.video.private)
		if err != nil {
			return fmt.Errorf("%w: hvcC: %v", ErrUnsupported, err)
		}
		if err := vt.SetHEVCDescriptor("hvc1", rec.GetNalusForType(hevc.NALU_VPS),
			rec.GetNalusForType(hevc.NALU_SPS), rec.GetNalusForType(hevc.NALU_PPS), nil, true); err != nil {
			return fmt.Errorf("%w: hvcC: %v", ErrUnsupported, err)
		}
	}
	if a := m.audio; a != nil {
		var asc *aac.AudioSpecificConfig
		if len(a.private) > 0 {
			c, err := aac.DecodeAudioSpecificConfig(bytes.NewReader(a.private))
			if err != nil {
				return fmt.Errorf("%w: AAC config: %v", ErrUnsupported, err)
			}
			asc = c
		} else {
			// Old "A_AAC/MPEG4/LC"-style IDs carry no config: build one.
			asc = &aac.AudioSpecificConfig{ObjectType: aac.AAClc, ChannelConfiguration: byte(a.channels), SamplingFrequency: int(a.rate)}
		}
		if asc.SamplingFrequency <= 0 {
			return fmt.Errorf("%w: AAC sampling frequency unknown", ErrUnsupported)
		}
		m.audioRate = asc.SamplingFrequency
		at := init.AddEmptyTrack(uint32(m.audioRate), "audio", "und")
		m.aID = at.Tkhd.TrackID
		if err := at.SetAACDescriptorFromConfig(asc); err != nil {
			return fmt.Errorf("%w: AAC config: %v", ErrUnsupported, err)
		}
		frame := int64(1024)
		if asc.FrameLengthFlag {
			frame = 960
		}
		m.audioFrame = frame * 1e9 / int64(m.audioRate) / m.d.scale
	}
	init.Ftyp = mp4.NewFtyp("isom", 0x200, brands)
	init.Children[0] = init.Ftyp
	m.lastVDTS, m.lastADT = -1, -1
	return init.Encode(m.w)
}

func (m *muxer) add(b block, startTicks int64) error {
	switch {
	case m.video != nil && b.track == m.video.number:
		if !m.started {
			if !b.key || b.pts < startTicks {
				return nil
			}
			m.started, m.base = true, b.pts
			for _, a := range m.pending {
				if a.pts >= m.base {
					m.abuf = append(m.abuf, a)
					m.bufBytes += len(a.data)
				}
			}
			m.pending = nil
		} else if b.key && len(m.vbuf) > 0 {
			if err := m.flush(false); err != nil {
				return err
			}
		}
		for _, f := range b.frames {
			data, err := m.toLength4(f)
			if err != nil {
				return err
			}
			m.vbuf = append(m.vbuf, sample{pts: b.pts, key: b.key, data: data})
			m.bufBytes += len(data)
		}
	case m.audio != nil && b.track == m.audio.number:
		if m.started && b.pts < m.base {
			return nil
		}
		for i, f := range b.frames {
			s := sample{pts: b.pts + int64(i)*m.audioFrame, key: true, data: f}
			if !m.started {
				// Keep a little audio in case it precedes the keyframe in
				// the file; anything older is dropped.
				if len(m.pending) >= 512 {
					m.pending = m.pending[1:]
				}
				m.pending = append(m.pending, s)
				continue
			}
			m.abuf = append(m.abuf, s)
			m.bufBytes += len(f)
		}
	default:
		return nil
	}
	if m.bufBytes > maxFragment {
		return m.flush(false)
	}
	return nil
}

// toLength4 rewrites NAL length prefixes to 4 bytes, which the MP4 sample
// entry declares.
func (m *muxer) toLength4(f []byte) ([]byte, error) {
	if m.nalLen == 4 {
		return f, nil
	}
	out := make([]byte, 0, len(f)+len(f)/16+8)
	for len(f) > 0 {
		if len(f) < m.nalLen {
			return nil, errors.New("truncated NAL unit")
		}
		n := int(readUint(f[:m.nalLen]))
		f = f[m.nalLen:]
		if n > len(f) {
			return nil, errors.New("truncated NAL unit")
		}
		out = binary.BigEndian.AppendUint32(out, uint32(n))
		out = append(out, f[:n]...)
		f = f[n:]
	}
	return out, nil
}

func (m *muxer) toTimescale(ticks int64, timescale int64) int64 {
	return (ticks - m.base) * m.d.scale * timescale / 1e9
}

// flush writes the buffered GOP and audio as one fragment.
func (m *muxer) flush(last bool) error {
	if len(m.vbuf) == 0 && len(m.abuf) == 0 {
		return nil
	}
	m.seq++
	var ids []uint32
	if len(m.vbuf) > 0 {
		ids = append(ids, m.vID)
	}
	if len(m.abuf) > 0 {
		ids = append(ids, m.aID)
	}
	frag, err := mp4.CreateMultiTrackFragment(m.seq, ids)
	if err != nil {
		return err
	}

	if n := len(m.vbuf); n > 0 {
		// Matroska stores presentation times in decode order. Decode times
		// are the sorted presentation times, kept strictly increasing.
		pts := make([]int64, n)
		for i, s := range m.vbuf {
			pts[i] = m.toTimescale(s.pts, videoTimescale)
		}
		dts := append([]int64(nil), pts...)
		sort.Slice(dts, func(i, j int) bool { return dts[i] < dts[j] })
		if !m.delaySet {
			for i := range dts {
				if d := dts[i] - pts[i]; d > m.delay {
					m.delay = d
				}
			}
			m.delaySet = true
		}
		for i := range pts {
			pts[i] += m.delay
		}
		for i := range dts {
			if dts[i] <= m.lastVDTS {
				dts[i] = m.lastVDTS + 1
			}
			m.lastVDTS = dts[i]
		}
		for i, s := range m.vbuf {
			dur := m.lastVDur
			if i+1 < n {
				dur = dts[i+1] - dts[i]
			} else if dur <= 0 {
				dur = int64(videoTimescale / 25)
				if m.video.defaultDur > 0 {
					dur = int64(m.video.defaultDur) * videoTimescale / 1e9
				}
			}
			if i+1 < n {
				m.lastVDur = dur
			}
			flags := mp4.NonSyncSampleFlags
			if s.key {
				flags = mp4.SyncSampleFlags
			}
			fs := mp4.FullSample{
				Sample:     mp4.Sample{Flags: flags, Dur: uint32(dur), Size: uint32(len(s.data)), CompositionTimeOffset: int32(pts[i] - dts[i])},
				DecodeTime: uint64(dts[i]),
				Data:       s.data,
			}
			if err := frag.AddFullSampleToTrack(fs, m.vID); err != nil {
				return err
			}
		}
		// The next fragment starts after this one's last sample.
		m.lastVDTS = dts[n-1] + m.lastVDur - 1
	}

	if n := len(m.abuf); n > 0 {
		rate := int64(m.audioRate)
		for i, s := range m.abuf {
			dt := m.toTimescale(s.pts, rate) + m.delay*rate/videoTimescale
			if dt <= m.lastADT {
				dt = m.lastADT + 1
			}
			var dur int64
			if i+1 < n {
				dur = m.toTimescale(m.abuf[i+1].pts, rate) + m.delay*rate/videoTimescale - dt
			}
			if dur <= 0 {
				dur = m.lastADur
				if dur <= 0 {
					dur = 1024
				}
			} else {
				m.lastADur = dur
			}
			fs := mp4.FullSample{
				Sample:     mp4.Sample{Flags: mp4.SyncSampleFlags, Dur: uint32(dur), Size: uint32(len(s.data))},
				DecodeTime: uint64(dt),
				Data:       s.data,
			}
			if err := frag.AddFullSampleToTrack(fs, m.aID); err != nil {
				return err
			}
			m.lastADT = dt + dur - 1
		}
	}

	m.vbuf, m.abuf, m.bufBytes = m.vbuf[:0], m.abuf[:0], 0
	if err := frag.Encode(m.w); err != nil {
		return err
	}
	if f, ok := m.w.(interface{ Flush() }); ok {
		f.Flush()
	}
	_ = last
	return nil
}
