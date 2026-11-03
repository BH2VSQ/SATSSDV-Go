package ax25

// StreamDecoder extracts AX.25 UI frames from a continuous NRZI level stream.
// It performs incremental HDLC flag detection and keeps only the current frame,
// so a long-running receiver has bounded memory and does not repeatedly scan
// the complete capture history.
type StreamDecoder struct {
	previousLevel bool
	haveLevel     bool
	inFrame       bool
	searchWindow  []bool
	frameBits     []bool
}

const maxStreamFrameBits = 8192

func (d *StreamDecoder) Reset() {
	if d == nil {
		return
	}
	d.previousLevel = false
	d.haveLevel = false
	d.inFrame = false
	d.searchWindow = nil
	d.frameBits = nil
}

func (d *StreamDecoder) Feed(levels []bool) []UIFrame {
	if d == nil || len(levels) == 0 {
		return nil
	}
	var frames []UIFrame
	for _, level := range levels {
		if !d.haveLevel {
			// NRZI's first bit depends on the unknown level before acquisition.
			// Discard it; subsequent bits are independent of this initial state.
			d.previousLevel = level
			d.haveLevel = true
			continue
		}
		bit := level == d.previousLevel
		d.previousLevel = level
		if !d.inFrame {
			d.searchWindow = append(d.searchWindow, bit)
			if len(d.searchWindow) > 8 {
				d.searchWindow = d.searchWindow[len(d.searchWindow)-8:]
			}
			if len(d.searchWindow) == 8 && equalFlag(d.searchWindow) {
				d.inFrame = true
				d.searchWindow = nil
				d.frameBits = d.frameBits[:0]
			}
			continue
		}

		d.frameBits = append(d.frameBits, bit)
		if len(d.frameBits) >= 8 && equalFlag(d.frameBits[len(d.frameBits)-8:]) {
			payloadBits := d.frameBits[:len(d.frameBits)-8]
			if len(payloadBits) > 0 {
				if raw, err := unstuff(payloadBits); err == nil && len(raw) >= 18 {
					if frame, parseErr := parseUIFrame(raw); parseErr == nil {
						frames = append(frames, frame)
					}
				}
			}
			// The closing flag is also the opening flag for the next frame.
			d.frameBits = d.frameBits[:0]
			continue
		}
		if len(d.frameBits) > maxStreamFrameBits {
			// Abandon a damaged/oversized frame and reacquire from its recent tail.
			d.inFrame = false
			d.searchWindow = append(d.searchWindow[:0], d.frameBits[len(d.frameBits)-7:]...)
			d.frameBits = d.frameBits[:0]
		}
	}
	return frames
}
