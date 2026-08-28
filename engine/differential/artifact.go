package differential

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
)

// THIS DECODER IS WRITTEN FROM docs/FORMAT-differential.md AND FROM NOTHING
// ELSE. It is deliberately not a port of the C++ one; see the package doc.

var magic = [8]byte{'R', 'I', 'F', 'T', 'D', 'I', 'F', 0}

const formatVersion = 1

const (
	headerBytes = 8 + 4 + 4
	footerBytes = 8 + 4
)

// Section kinds. All five are required, in ascending order.
const (
	sectionProvenance = 1
	sectionSubmission = 2
	sectionWatermark  = 3
	sectionRecovered  = 4
	sectionVerdict    = 5

	firstSection = sectionProvenance
	lastSection  = sectionVerdict
)

// OpKind mirrors the format's operation kinds.
type OpKind uint8

const (
	OpSet OpKind = iota + 1
	OpDelete
	OpDeleteRange
	OpSync
	OpSnapshotTake
	OpSnapshotRelease
)

// Outcome is the verdict a judge reached. Unrun means the artifact has not been
// judged, which is legal in the format and refused at the corpus gate.
type Outcome uint8

const (
	Unrun Outcome = iota
	Agree
	RecoveredLess
	RecoveredMore
	RecoveredNeither
)

func (o Outcome) String() string {
	switch o {
	case Unrun:
		return "unrun"
	case Agree:
		return "agree"
	case RecoveredLess:
		return "recovered less than promised"
	case RecoveredMore:
		return "recovered more than promised"
	case RecoveredNeither:
		return "recovered a state at no watermark"
	}
	return fmt.Sprintf("outcome(%d)", uint8(o))
}

// Op is one entry of the submission log.
type Op struct {
	Kind OpKind
	Seq  uint64
	Key  []byte
	// Value for OpSet; the range end for OpDeleteRange.
	Value []byte
	// An empty key is a valid key, so boundedness cannot be carried by
	// emptiness — the same distinction engine.Batch carries with nil.
	StartBounded bool
	EndBounded   bool
}

// Provenance names what produced the artifact and what it must be judged
// against.
type Provenance struct {
	EngineCommit string
	ModelCommit  string
	// Regime is required and is not derivable from the caps beside it: two
	// regimes can share caps and differ in workload.
	Regime         string
	Seed           uint64
	FlushBytes     uint64
	WALBufferBytes uint64
	MaxRecordBytes uint64
}

// Artifact is one differential run.
type Artifact struct {
	Provenance Provenance
	Submission []Op
	Watermark  uint64
	Recovered  map[string][]byte
	// RecoveredKeys preserves the file's order, so the ascending-order rule can
	// be checked and so a re-encode is byte-identical.
	RecoveredKeys []string
	Outcome       Outcome
	Why           string
}

// Errors are values so a test can assert which refusal fired rather than
// matching on a message.
var (
	ErrTooSmall             = errors.New("differential: smaller than a header and a footer")
	ErrBadMagic             = errors.New("differential: not a differential artifact")
	ErrBadTrailingMagic     = errors.New("differential: incomplete: the file ends before its footer")
	ErrBadChecksum          = errors.New("differential: checksum mismatch")
	ErrUnknownVersion       = errors.New("differential: format version this build cannot place")
	ErrSectionPastEnd       = errors.New("differential: a section runs past the footer")
	ErrUnknownSection       = errors.New("differential: unknown section kind")
	ErrDuplicateSection     = errors.New("differential: two sections of one kind")
	ErrMissingSection       = errors.New("differential: a required section is absent")
	ErrSectionsNotAscend    = errors.New("differential: sections are not in ascending kind order")
	ErrTrailingBytes        = errors.New("differential: bytes after the footer")
	ErrEmptySubmission      = errors.New("differential: an artifact of no operations")
	ErrRecoveredNotAscend   = errors.New("differential: recovered keys are not strictly ascending")
	ErrUnknownOutcome       = errors.New("differential: verdict names no enumerator")
	ErrMissingCommit        = errors.New("differential: provenance names no commit")
	ErrDirtyCommit          = errors.New("differential: provenance names an uncommitted tree")
	ErrMissingRegime        = errors.New("differential: provenance names no regime")
	ErrSequencesNotMonotone = errors.New("differential: submission sequences are not non-decreasing")
	ErrMalformedSection     = errors.New("differential: a section's payload is malformed")
	ErrUnjudged             = errors.New("differential: artifact has not been judged")
)

// crc32c, the same polynomial the C++ side uses. One checksum in this project.
var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// cursor bounds every read before it makes it.
type cursor struct{ b []byte }

func (c *cursor) u8() (uint8, bool) {
	if len(c.b) < 1 {
		return 0, false
	}
	v := c.b[0]
	c.b = c.b[1:]
	return v, true
}

func (c *cursor) u32() (uint32, bool) {
	if len(c.b) < 4 {
		return 0, false
	}
	v := binary.LittleEndian.Uint32(c.b)
	c.b = c.b[4:]
	return v, true
}

func (c *cursor) u64() (uint64, bool) {
	if len(c.b) < 8 {
		return 0, false
	}
	v := binary.LittleEndian.Uint64(c.b)
	c.b = c.b[8:]
	return v, true
}

func (c *cursor) str() ([]byte, bool) {
	n, ok := c.u32()
	if !ok || uint64(len(c.b)) < uint64(n) {
		return nil, false
	}
	v := c.b[:n]
	c.b = c.b[n:]
	return v, true
}

func (c *cursor) done() bool { return len(c.b) == 0 }

// Parse decodes and checks an artifact, from bytes alone. It accepts an
// UNJUDGED artifact; see RequireJudged.
func Parse(image []byte) (*Artifact, error) {
	if len(image) < headerBytes+footerBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooSmall, len(image))
	}
	if string(image[:8]) != string(magic[:]) {
		return nil, ErrBadMagic
	}
	// THE TRAILING MAGIC IS CHECKED BEFORE ANYTHING IS FOLLOWED, so a truncated
	// file is reported as INCOMPLETE rather than discovered later as a section
	// overrun — which would report "malformed" about a file that is not.
	footerAt := len(image) - footerBytes
	if string(image[footerAt:footerAt+8]) != string(magic[:]) {
		return nil, ErrBadTrailingMagic
	}
	crcAt := len(image) - 4
	if crc32.Checksum(image[:crcAt], castagnoli) != binary.LittleEndian.Uint32(image[crcAt:]) {
		return nil, ErrBadChecksum
	}
	if v := binary.LittleEndian.Uint32(image[8:12]); v != formatVersion {
		return nil, fmt.Errorf("%w: version %d", ErrUnknownVersion, v)
	}
	count := binary.LittleEndian.Uint32(image[12:16])

	a := &Artifact{Recovered: map[string][]byte{}}
	seen := [lastSection + 1]bool{}
	at := headerBytes
	prevKind := 0
	for i := uint32(0); i < count; i++ {
		if at+5 > footerAt {
			return nil, ErrSectionPastEnd
		}
		kind := int(image[at])
		length := int(binary.LittleEndian.Uint32(image[at+1 : at+5]))
		payloadAt := at + 5
		if payloadAt+length > footerAt {
			return nil, ErrSectionPastEnd
		}
		// The range check comes first, because the byte came off disk.
		if kind < firstSection || kind > lastSection {
			// REFUSED, NOT SKIPPED: an artifact whose unknown section is
			// ignored has lost the thing it was meant to carry and reports
			// success.
			return nil, fmt.Errorf("%w: %d", ErrUnknownSection, kind)
		}
		if seen[kind] {
			return nil, ErrDuplicateSection
		}
		if kind <= prevKind {
			return nil, ErrSectionsNotAscend
		}
		seen[kind] = true
		prevKind = kind

		c := &cursor{b: image[payloadAt : payloadAt+length]}
		if err := a.readSection(kind, c); err != nil {
			return nil, err
		}
		at = payloadAt + length
	}
	if at != footerAt {
		return nil, ErrTrailingBytes
	}
	for k := firstSection; k <= lastSection; k++ {
		if !seen[k] {
			return nil, fmt.Errorf("%w: kind %d", ErrMissingSection, k)
		}
	}
	return a, nil
}

func (a *Artifact) readSection(kind int, c *cursor) error {
	switch kind {
	case sectionProvenance:
		engineCommit, ok1 := c.str()
		modelCommit, ok2 := c.str()
		regime, ok3 := c.str()
		seed, ok4 := c.u64()
		flush, ok5 := c.u64()
		buf, ok6 := c.u64()
		rec, ok7 := c.u64()
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 || !c.done() {
			return fmt.Errorf("%w: provenance", ErrMalformedSection)
		}
		a.Provenance = Provenance{
			EngineCommit:   string(engineCommit),
			ModelCommit:    string(modelCommit),
			Regime:         string(regime),
			Seed:           seed,
			FlushBytes:     flush,
			WALBufferBytes: buf,
			MaxRecordBytes: rec,
		}
		if a.Provenance.EngineCommit == "" || a.Provenance.ModelCommit == "" {
			return ErrMissingCommit
		}
		if strings.HasSuffix(a.Provenance.EngineCommit, "-dirty") ||
			strings.HasSuffix(a.Provenance.ModelCommit, "-dirty") {
			return ErrDirtyCommit
		}
		if a.Provenance.Regime == "" {
			return ErrMissingRegime
		}
		return nil

	case sectionSubmission:
		n, ok := c.u32()
		if !ok {
			return fmt.Errorf("%w: submission count", ErrMalformedSection)
		}
		if n == 0 {
			return ErrEmptySubmission
		}
		var prevSeq uint64
		for i := uint32(0); i < n; i++ {
			k, ok1 := c.u8()
			seq, ok2 := c.u64()
			key, ok3 := c.str()
			val, ok4 := c.str()
			flags, ok5 := c.u8()
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
				return fmt.Errorf("%w: operation %d", ErrMalformedSection, i)
			}
			if k < uint8(OpSet) || k > uint8(OpSnapshotRelease) {
				return fmt.Errorf("%w: operation kind %d", ErrMalformedSection, k)
			}
			// Zero is exempt: Sync and the snapshot ops consume no sequence.
			if seq != 0 && seq < prevSeq {
				return ErrSequencesNotMonotone
			}
			if seq != 0 {
				prevSeq = seq
			}
			a.Submission = append(a.Submission, Op{
				Kind:         OpKind(k),
				Seq:          seq,
				Key:          append([]byte(nil), key...),
				Value:        append([]byte(nil), val...),
				StartBounded: flags&1 != 0,
				EndBounded:   flags&2 != 0,
			})
		}
		if !c.done() {
			return fmt.Errorf("%w: trailing bytes in submission", ErrMalformedSection)
		}
		return nil

	case sectionWatermark:
		w, ok := c.u64()
		if !ok || !c.done() {
			return fmt.Errorf("%w: watermark", ErrMalformedSection)
		}
		a.Watermark = w
		return nil

	case sectionRecovered:
		n, ok := c.u32()
		if !ok {
			return fmt.Errorf("%w: recovered count", ErrMalformedSection)
		}
		var prev string
		havePrev := false
		for i := uint32(0); i < n; i++ {
			k, ok1 := c.str()
			v, ok2 := c.str()
			if !ok1 || !ok2 {
				return fmt.Errorf("%w: recovered entry", ErrMalformedSection)
			}
			key := string(k)
			if havePrev && !(prev < key) {
				return ErrRecoveredNotAscend
			}
			prev, havePrev = key, true
			a.Recovered[key] = append([]byte(nil), v...)
			a.RecoveredKeys = append(a.RecoveredKeys, key)
		}
		if !c.done() {
			return fmt.Errorf("%w: trailing bytes in recovered", ErrMalformedSection)
		}
		return nil

	case sectionVerdict:
		o, ok1 := c.u8()
		why, ok2 := c.str()
		if !ok1 || !ok2 || !c.done() {
			return fmt.Errorf("%w: verdict", ErrMalformedSection)
		}
		if o > uint8(RecoveredNeither) {
			return fmt.Errorf("%w: outcome %d", ErrUnknownOutcome, o)
		}
		a.Outcome = Outcome(o)
		a.Why = string(why)
		return nil
	}
	panic("unreachable: section kind was range-checked")
}

// RequireJudged is the corpus gate. A file under seeds/differential/ must have
// been judged, or it cannot reproduce a finding — B4-Q3's strict form. It is
// separate from Parse because it is a rule about what the corpus holds, not
// about what the format admits.
func RequireJudged(a *Artifact) error {
	if a.Outcome == Unrun {
		return ErrUnjudged
	}
	return nil
}
