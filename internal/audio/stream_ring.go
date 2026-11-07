package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"sync/atomic"
)

// float32Ring is a single-producer/single-consumer bounded PCM queue. The TX
// encoder owns the write side; the miniaudio callback owns the read side.
// Capacity is fixed for the lifetime of one burst, so memory usage is bounded
// independently of transmission duration.
type float32Ring struct {
	data  []float32
	read  atomic.Uint64
	write atomic.Uint64
	space chan struct{}
}

func newFloat32Ring(capacity int) (*float32Ring, error) {
	if capacity < 2 {
		return nil, errors.New("PCM ring capacity must be at least two samples")
	}
	return &float32Ring{data: make([]float32, capacity), space: make(chan struct{}, 1)}, nil
}

func (r *float32Ring) Len() int {
	if r == nil {
		return 0
	}
	read, write := r.read.Load(), r.write.Load()
	used := write - read
	if used > uint64(len(r.data)) {
		return len(r.data)
	}
	return int(used)
}

func (r *float32Ring) Write(ctx context.Context, samples []float32) error {
	if r == nil {
		return errors.New("PCM ring is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for offset := 0; offset < len(samples); {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, write := r.read.Load(), r.write.Load()
		used := write - read
		if used >= uint64(len(r.data)) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-r.space:
			}
			continue
		}
		available := len(r.data) - int(used)
		count := min(available, len(samples)-offset)
		index := int(write % uint64(len(r.data)))
		first := min(count, len(r.data)-index)
		copy(r.data[index:index+first], samples[offset:offset+first])
		if first < count {
			copy(r.data[:count-first], samples[offset+first:offset+count])
		}
		r.write.Store(write + uint64(count))
		offset += count
	}
	return nil
}

// ReadToBytes writes up to len(dst)/4 PCM samples as little-endian float32.
// The caller must zero dst first to make any underrun region silent.
func (r *float32Ring) ReadToBytes(dst []byte) int {
	if r == nil || len(dst) < 4 {
		return 0
	}
	read, write := r.read.Load(), r.write.Load()
	available := int(write - read)
	count := min(available, len(dst)/4)
	for i := 0; i < count; i++ {
		value := r.data[(int(read)+i)%len(r.data)]
		binary.LittleEndian.PutUint32(dst[i*4:i*4+4], math.Float32bits(value))
	}
	r.read.Store(read + uint64(count))
	if count > 0 {
		select {
		case r.space <- struct{}{}:
		default:
		}
	}
	return count
}

func (r *float32Ring) Clear() {
	if r == nil {
		return
	}
	r.read.Store(r.write.Load())
	select {
	case r.space <- struct{}{}:
	default:
	}
}
