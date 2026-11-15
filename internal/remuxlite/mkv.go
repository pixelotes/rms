package remuxlite

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

type track struct {
	number     uint64
	typ        uint64 // 1 video, 2 audio
	codec      string
	private    []byte
	enabled    bool
	isDefault  bool
	width      uint64
	height     uint64
	rate       float64
	channels   uint64
	defaultDur uint64 // ns
	language   string
	encoded    bool // ContentEncodings (compression or header stripping)
}

// block is one Matroska block: frames of a track at a presentation time.
type block struct {
	track  uint64
	pts    int64 // in timecode-scale ticks
	key    bool
	frames [][]byte
}

// demuxer reads Matroska headers and then blocks, in file order.
type demuxer struct {
	r        *reader
	docType  string
	scale    int64 // nanoseconds per timecode tick
	tracks   []*track
	pending  *elemHeader // header read past the end of an unknown-size cluster
	inClu    bool
	cluEnd   int64 // -1 when unknown
	cluTC    int64
	queue    []block
	segEnd   int64 // -1 when unknown
	finished bool
}

type elemHeader struct {
	id   uint32
	size int64
}

// openDemuxer parses everything up to the first cluster.
func openDemuxer(src io.Reader) (*demuxer, error) {
	d := &demuxer{r: newReader(src), scale: 1000000, segEnd: -1}
	id, size, err := d.r.header()
	if err != nil {
		return nil, fmt.Errorf("not a Matroska file: %w", err)
	}
	if id != idEBML || size == unknownSize {
		return nil, errors.New("not a Matroska file")
	}
	if err := d.r.children(size, func(id uint32, sz int64) error {
		if id == idDocType {
			b, err := d.r.readFull(sz)
			d.docType = string(b)
			return err
		}
		return d.r.skip(sz)
	}); err != nil {
		return nil, err
	}
	if d.docType != "matroska" && d.docType != "webm" {
		return nil, fmt.Errorf("unsupported DocType %q", d.docType)
	}
	id, size, err = d.r.header()
	if err != nil {
		return nil, err
	}
	if id != idSegment {
		return nil, errors.New("no Segment element")
	}
	if size != unknownSize {
		d.segEnd = d.r.pos + size
	}
	for {
		id, size, err := d.r.header()
		if err != nil {
			return nil, fmt.Errorf("reading headers: %w", err)
		}
		switch id {
		case idCluster:
			d.pending = &elemHeader{id, size}
			if d.tracks == nil {
				return nil, errors.New("no Tracks element before the first Cluster")
			}
			return d, nil
		case idInfo:
			if err := d.r.children(size, func(id uint32, sz int64) error {
				if id == idTimecodeScale {
					b, err := d.r.readFull(sz)
					if v := int64(readUint(b)); v > 0 {
						d.scale = v
					}
					return err
				}
				return d.r.skip(sz)
			}); err != nil {
				return nil, err
			}
		case idTracks:
			if err := d.readTracks(size); err != nil {
				return nil, err
			}
		default:
			if size == unknownSize {
				return nil, fmt.Errorf("unknown-size element %#x", id)
			}
			if err := d.r.skip(size); err != nil {
				return nil, err
			}
		}
	}
}

func (d *demuxer) readTracks(size int64) error {
	return d.r.children(size, func(id uint32, sz int64) error {
		if id != idTrackEntry {
			return d.r.skip(sz)
		}
		t := &track{enabled: true, isDefault: true, language: "eng"}
		err := d.r.children(sz, func(id uint32, sz int64) error {
			switch id {
			case idVideo:
				return d.r.children(sz, func(id uint32, sz int64) error {
					b, err := d.r.readFull(sz)
					switch id {
					case idPixelWidth:
						t.width = readUint(b)
					case idPixelHeight:
						t.height = readUint(b)
					}
					return err
				})
			case idAudio:
				return d.r.children(sz, func(id uint32, sz int64) error {
					b, err := d.r.readFull(sz)
					switch id {
					case idSamplingFreq:
						t.rate = readFloat(b)
					case idChannels:
						t.channels = readUint(b)
					}
					return err
				})
			case idContentEncs:
				t.encoded = true
				return d.r.skip(sz)
			}
			b, err := d.r.readFull(sz)
			if err != nil {
				return err
			}
			switch id {
			case idTrackNumber:
				t.number = readUint(b)
			case idTrackType:
				t.typ = readUint(b)
			case idFlagEnabled:
				t.enabled = readUint(b) != 0
			case idFlagDefault:
				t.isDefault = readUint(b) != 0
			case idCodecID:
				t.codec = string(b)
			case idCodecPrivate:
				t.private = b
			case idDefaultDur:
				t.defaultDur = readUint(b)
			case idLanguage:
				t.language = string(b)
			}
			return nil
		})
		if err != nil {
			return err
		}
		d.tracks = append(d.tracks, t)
		return nil
	})
}

// next returns the next block, or io.EOF at the end of the file.
func (d *demuxer) next() (block, error) {
	for len(d.queue) == 0 {
		if d.finished {
			return block{}, io.EOF
		}
		if err := d.step(); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				// A truncated tail (an interrupted download) still plays.
				d.finished = true
				continue
			}
			return block{}, err
		}
	}
	b := d.queue[0]
	d.queue = d.queue[1:]
	return b, nil
}

func (d *demuxer) readHeader() (uint32, int64, error) {
	if h := d.pending; h != nil {
		d.pending = nil
		return h.id, h.size, nil
	}
	return d.r.header()
}

// step reads one element and queues any block found in it.
func (d *demuxer) step() error {
	if d.inClu && d.cluEnd >= 0 && d.r.pos >= d.cluEnd {
		d.inClu = false
	}
	if !d.inClu && d.segEnd >= 0 && d.r.pos >= d.segEnd && d.pending == nil {
		return io.EOF
	}
	id, size, err := d.readHeader()
	if err != nil {
		return err
	}
	if d.inClu && d.cluEnd < 0 && topLevel[id] {
		d.inClu = false // an unknown-size cluster ends at the next top-level element
	}
	if !d.inClu {
		if id == idCluster {
			d.inClu, d.cluTC = true, 0
			d.cluEnd = -1
			if size != unknownSize {
				d.cluEnd = d.r.pos + size
			}
			return nil
		}
		if size == unknownSize {
			return fmt.Errorf("unknown-size element %#x", id)
		}
		return d.r.skip(size)
	}
	if size == unknownSize {
		return fmt.Errorf("unknown-size element %#x in a cluster", id)
	}
	switch id {
	case idTimecode:
		b, err := d.r.readFull(size)
		d.cluTC = int64(readUint(b))
		return err
	case idSimpleBlock:
		data, err := d.r.readFull(size)
		if err != nil {
			return err
		}
		return d.queueBlock(data, true, false)
	case idBlockGroup:
		var data []byte
		hasRef := false
		if err := d.r.children(size, func(id uint32, sz int64) error {
			switch id {
			case idBlock:
				b, err := d.r.readFull(sz)
				data = b
				return err
			case idReferenceBlock:
				hasRef = true
			}
			return d.r.skip(sz)
		}); err != nil {
			return err
		}
		if data == nil {
			return nil
		}
		return d.queueBlock(data, false, !hasRef)
	default:
		return d.r.skip(size)
	}
}

// queueBlock parses a (Simple)Block payload. For a SimpleBlock the keyframe
// flag is in the header; for a Block it comes from the absence of references.
func (d *demuxer) queueBlock(data []byte, simple, groupKey bool) error {
	num, n, err := vint(data)
	if err != nil {
		return err
	}
	if len(data) < n+3 {
		return errors.New("short block")
	}
	rel := int64(int16(binary.BigEndian.Uint16(data[n:])))
	flags := data[n+2]
	payload := data[n+3:]
	key := groupKey
	if simple {
		key = flags&0x80 != 0
	}
	frames, err := unlace(payload, (flags>>1)&3)
	if err != nil {
		return err
	}
	d.queue = append(d.queue, block{track: num, pts: d.cluTC + rel, key: key, frames: frames})
	return nil
}

// unlace splits a block payload into frames (lacing: 0 none, 1 Xiph,
// 2 fixed-size, 3 EBML).
func unlace(p []byte, lacing byte) ([][]byte, error) {
	if lacing == 0 {
		return [][]byte{p}, nil
	}
	if len(p) < 1 {
		return nil, errors.New("short laced block")
	}
	count := int(p[0]) + 1
	p = p[1:]
	sizes := make([]int, count)
	switch lacing {
	case 1: // Xiph
		for i := 0; i < count-1; i++ {
			for {
				if len(p) == 0 {
					return nil, errors.New("bad Xiph lacing")
				}
				c := p[0]
				p = p[1:]
				sizes[i] += int(c)
				if c != 255 {
					break
				}
			}
		}
	case 2: // fixed
		if len(p)%count != 0 {
			return nil, errors.New("bad fixed lacing")
		}
		for i := range sizes[:count-1] {
			sizes[i] = len(p) / count
		}
	case 3: // EBML
		v, n, err := vint(p)
		if err != nil {
			return nil, err
		}
		sizes[0] = int(v)
		p = p[n:]
		for i := 1; i < count-1; i++ {
			v, n, err := vint(p)
			if err != nil {
				return nil, err
			}
			// signed: subtract the bias for an n-byte vint
			diff := int64(v) - (int64(1)<<(7*n-1) - 1)
			sizes[i] = sizes[i-1] + int(diff)
			p = p[n:]
		}
	}
	total := 0
	for _, s := range sizes[:count-1] {
		if s < 0 {
			return nil, errors.New("bad lace size")
		}
		total += s
	}
	if total > len(p) {
		return nil, errors.New("lace sizes exceed block")
	}
	sizes[count-1] = len(p) - total
	frames := make([][]byte, count)
	for i, s := range sizes {
		frames[i] = p[:s]
		p = p[s:]
	}
	return frames, nil
}
