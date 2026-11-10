package audio

import "sync/atomic"

// enqueueCapturePCM preserves the FIFO history already accepted by the RX
// worker. When the bounded queue saturates, it drops only newly arriving PCM
// blocks and inserts one discontinuity marker before audio resumes. The caller
// is the single serialized capture-callback producer.
func enqueueCapturePCM(queue chan []float32, block []float32, gapPending *bool, dropped *atomic.Uint64) bool {
	if queue == nil || gapPending == nil || dropped == nil || len(block) == 0 {
		return false
	}
	if *gapPending {
		// Reserve one slot for the discontinuity marker and one for the first
		// block after the gap. If there is not enough room, keep the marker
		// pending and drop only this incoming callback block.
		if cap(queue)-len(queue) < 2 {
			dropped.Add(1)
			return false
		}
		queue <- nil
		*gapPending = false
	}
	select {
	case queue <- block:
		return true
	default:
		dropped.Add(1)
		*gapPending = true
		return false
	}
}
