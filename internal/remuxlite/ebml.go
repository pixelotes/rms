// Package remuxlite repackages Matroska (MKV/WebM) files into fragmented MP4
// without ffmpeg. It only copies streams: H.264 or HEVC video and AAC audio,
// which browsers decode natively. Anything else is reported as unsupported so
// the caller can fall back to ffmpeg.
package remuxlite

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// unknownSize marks an EBML element whose size is not stored (live-written
// files leave Segment and Cluster sizes open).
const unknownSize = -1

// Matroska element IDs used here.
const (
	idEBML           = 0x1A45DFA3
	idDocType        = 0x4282
	idSegment        = 0x18538067
	idSeekHead       = 0x114D9B74
	idInfo           = 0x1549A966
	idTimecodeScale  = 0x2AD7B1
	idTracks         = 0x1654AE6B
	idTrackEntry     = 0xAE
	idTrackNumber    = 0xD7
	idTrackType      = 0x83
	idFlagEnabled    = 0xB9
	idFlagDefault    = 0x88
	idCodecID        = 0x86
	idCodecPrivate   = 0x63A2
	idDefaultDur     = 0x23E383
	idLanguage       = 0x22B59C
	idContentEncs    = 0x6D80
	idVideo          = 0xE0
	idPixelWidth     = 0xB0
	idPixelHeight    = 0xBA
	idAudio          = 0xE1
	idSamplingFreq   = 0xB5
	idChannels       = 0x9F
	idCluster        = 0x1F43B675
	idTimecode       = 0xE7
	idSimpleBlock    = 0xA3
	idBlockGroup     = 0xA0
	idBlock          = 0xA1
	idBlockDuration  = 0x9B
	idReferenceBlock = 0xFB
	idCues           = 0x1C53BB6B
	idAttachments    = 0x1941A469
	idChapters       = 0x1043A770
	idTags           = 0x1254C367
)

// topLevel lists the Segment children. Inside an unknown-size Cluster, one
// of these IDs ends the cluster.
var topLevel = map[uint32]bool{
	idSeekHead: true, idInfo: true, idTracks: true, idCluster: true,
	idCues: true, idAttachments: true, idChapters: true, idTags: true,
}

// reader reads EBML elements from a stream and tracks the byte position.
type reader struct {
	r   *bufio.Reader
	pos int64
}

func newReader(r io.Reader) *reader {
	return &reader{r: bufio.NewReaderSize(r, 256<<10)}
}

func (r *reader) readByte() (byte, error) {
	b, err := r.r.ReadByte()
	if err == nil {
		r.pos++
	}
	return b, err
}

func (r *reader) readFull(n int64) ([]byte, error) {
	if n < 0 || n > maxElement {
		return nil, fmt.Errorf("element of %d bytes is too large", n)
	}
	buf := make([]byte, n)
	m, err := io.ReadFull(r.r, buf)
	r.pos += int64(m)
	if err == io.EOF && n > 0 {
		err = io.ErrUnexpectedEOF
	}
	return buf, err
}

func (r *reader) skip(n int64) error {
	m, err := r.r.Discard(int(n))
	r.pos += int64(m)
	if err == io.EOF && int64(m) < n {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// maxElement caps one element read into memory (a block or CodecPrivate).
const maxElement = 64 << 20

// readID reads an element ID (1–4 bytes, marker bits kept).
func (r *reader) readID() (uint32, error) {
	b, err := r.readByte()
	if err != nil {
		return 0, err
	}
	n := 0
	for mask := byte(0x80); n < 4 && b&mask == 0; mask >>= 1 {
		n++
	}
	if n >= 4 {
		return 0, errors.New("invalid EBML ID")
	}
	id := uint32(b)
	for i := 0; i < n; i++ {
		c, err := r.readByte()
		if err != nil {
			return 0, err
		}
		id = id<<8 | uint32(c)
	}
	return id, nil
}

// readSize reads an element data size; all-ones means unknown.
func (r *reader) readSize() (int64, error) {
	b, err := r.readByte()
	if err != nil {
		return 0, err
	}
	n := 0
	for mask := byte(0x80); n < 8 && b&mask == 0; mask >>= 1 {
		n++
	}
	if n >= 8 {
		return 0, errors.New("invalid EBML size")
	}
	v := uint64(b & (0xFF >> (n + 1)))
	allOnes := v == uint64(0xFF>>(n+1))
	for i := 0; i < n; i++ {
		c, err := r.readByte()
		if err != nil {
			return 0, err
		}
		v = v<<8 | uint64(c)
		allOnes = allOnes && c == 0xFF
	}
	if allOnes {
		return unknownSize, nil
	}
	if v > math.MaxInt64 {
		return 0, errors.New("EBML size overflow")
	}
	return int64(v), nil
}

func (r *reader) header() (id uint32, size int64, err error) {
	if id, err = r.readID(); err != nil {
		return 0, 0, err
	}
	size, err = r.readSize()
	return id, size, err
}

func readUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

func readFloat(b []byte) float64 {
	switch len(b) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(b))
	}
	return 0
}

// vint parses a Matroska variable-size integer (marker removed) from b.
func vint(b []byte) (v uint64, n int, err error) {
	if len(b) == 0 {
		return 0, 0, errors.New("empty vint")
	}
	for mask := byte(0x80); n < 8 && b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n >= 8 || len(b) < n+1 {
		return 0, 0, errors.New("invalid vint")
	}
	v = uint64(b[0] & (0xFF >> (n + 1)))
	for i := 1; i <= n; i++ {
		v = v<<8 | uint64(b[i])
	}
	return v, n + 1, nil
}

// children walks the children of an element of the given size, calling fn
// for each. fn must consume exactly the child's data (read or skip it).
func (r *reader) children(size int64, fn func(id uint32, size int64) error) error {
	end := r.pos + size
	for r.pos < end {
		id, sz, err := r.header()
		if err != nil {
			return err
		}
		if sz == unknownSize {
			return fmt.Errorf("unknown-size element %#x inside a sized element", id)
		}
		if err := fn(id, sz); err != nil {
			return err
		}
	}
	return nil
}
