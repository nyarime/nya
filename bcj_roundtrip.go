package nya

import "bytes"

// bcjRoundtripOK encodes then decodes with the same BCJ path used for archives.
// BCJ is only accepted when every byte matches the source — required for signed
// PE/Mach-O where Authenticode must survive extract.
func bcjRoundtripOK(raw []byte, arch string) bool {
	if arch == "" || len(raw) == 0 {
		return false
	}
	work := append([]byte(nil), raw...)
	ApplyBCJFilterArchSmart(work, arch, true)
	ApplyBCJFilterArchSmart(work, arch, false)
	return bytes.Equal(work, raw)
}

// tryBCJForArchive applies BCJ when roundtrip is exact and compression shrinks.
func (nw *Writer) tryBCJForArchive(raw []byte, bcjArch string, wholeStream bool) ([]byte, uint8, bool) {
	if bcjArch == "" || !bcjRoundtripOK(raw, bcjArch) {
		return raw, BCJNone, false
	}
	filtered := make([]byte, len(raw))
	copy(filtered, raw)
	if wholeStream {
		ApplyBCJFilterArch(filtered, bcjArch, true)
	} else {
		ApplyBCJFilterArchSmart(filtered, bcjArch, true)
	}

	var (
		origLen int
		err     error
	)
	if wholeStream {
		comp, err := nw.compressRaw(raw)
		if err != nil {
			return raw, BCJNone, false
		}
		origLen = len(comp)
	} else {
		origLen, err = nw.blockedCompressedLen(raw)
		if err != nil {
			return raw, BCJNone, false
		}
	}

	var compLen int
	if wholeStream {
		comp, err := nw.compressRaw(filtered)
		if err != nil {
			return raw, BCJNone, false
		}
		compLen = len(comp)
	} else {
		compLen, err = nw.blockedCompressedLen(filtered)
		if err != nil {
			return raw, BCJNone, false
		}
	}
	if compLen >= origLen {
		return raw, BCJNone, false
	}
	return filtered, BCJArchToID(bcjArch), true
}
