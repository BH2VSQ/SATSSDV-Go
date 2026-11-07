package mary

import (
	"fmt"
	"testing"
)

func TestMAModulationRoundTripAllConstellations(t *testing.T) {
	payload := make([]byte, 1537)
	for i := range payload {
		payload[i] = byte((i*73 + i/5 + 19) & 0xff)
	}
	for _, tc := range []struct {
		name      string
		bps, baud int
	}{
		{"QPSK-1600", 2, 1600}, {"8PSK-1600", 3, 1600}, {"16QAM-1600", 4, 1600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{SampleRate: 48000, SymbolRate: tc.baud, BitsPerSymbol: tc.bps, CarrierHz: tc.baud}
			wave, err := ModulateBytes(payload, cfg)
			if err != nil {
				t.Fatal(err)
			}
			for _, chunk := range []int{len(wave), 960, 1920, 4096} {
				d, err := NewStreamDemodulator(cfg)
				if err != nil {
					t.Fatal(err)
				}
				var bits []bool
				for i := 0; i < len(wave); i += chunk {
					end := i + chunk
					if end > len(wave) {
						end = len(wave)
					}
					bits = append(bits, d.Feed(wave[i:end])...)
				}
				got := bitsToBytes(bits)
				if len(got) < len(payload) {
					t.Fatalf("chunk %d recovered %d bytes, want >=%d", chunk, len(got), len(payload))
				}
				// The last byte may contain symbol padding for 8PSK; the original
				// byte stream must remain exact at its full-byte boundary.
				for j := range payload {
					if got[j] != payload[j] {
						t.Fatalf("chunk %d byte %d: got %02x want %02x", chunk, j, got[j], payload[j])
					}
				}
			}
		})
	}
}

func TestMAModulationFrequencyOffsetAcquisition(t *testing.T) {
	payload := []byte("SATSSDV-QPSK-8PSK-16QAM-frequency-offset-regression")
	for _, offset := range []int{80, -80} {
		for _, bps := range []int{2, 3, 4} {
			t.Run(fmt.Sprintf("1600baud_bps%d_offset%+d", bps, offset), func(t *testing.T) {
				txCfg := Config{SampleRate: 48000, SymbolRate: 1600, BitsPerSymbol: bps, CarrierHz: 1600 + offset}
				rxCfg := txCfg
				rxCfg.CarrierHz = 1600
				wave, err := ModulateBytes(payload, txCfg)
				if err != nil {
					t.Fatal(err)
				}
				input := make([]float32, 13+12000) // 250 ms of idle audio before the burst.
				input = append(input, wave...)
				for _, chunk := range []int{480, 960, 1920, 4096} {
					d, err := NewStreamDemodulator(rxCfg)
					if err != nil {
						t.Fatal(err)
					}
					var bits []bool
					for i := 0; i < len(input); i += chunk {
						end := i + chunk
						if end > len(input) {
							end = len(input)
						}
						bits = append(bits, d.Feed(input[i:end])...)
					}
					got := bitsToBytes(bits)
					if len(got) < len(payload) {
						t.Fatalf("chunk %d: %d-bit constellation did not acquire after offset/idle audio; recovered %d bytes", chunk, bps, len(got))
					}
					for i := range payload {
						if got[i] != payload[i] {
							t.Fatalf("chunk %d: %d-bit offset decode byte %d: got %02x want %02x", chunk, bps, i, got[i], payload[i])
						}
					}
				}
			})
		}
	}
}

func TestMAModulationAcquiresAtArbitrarySampleOffsetsAfterIdle(t *testing.T) {
	payload := []byte("SATSSDV-MARY-ARBITRARY-SYMBOL-BOUNDARY")
	cfg := Config{SampleRate: 48000, SymbolRate: 1600, CarrierHz: 1600}
	for _, bps := range []int{2, 3, 4} {
		cfg.BitsPerSymbol = bps
		wave, err := ModulateBytes(payload, cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, preRoll := range []int{13, 2413, 12013} {
			for _, chunk := range []int{480, 960, 1920} {
				t.Run(fmt.Sprintf("bps%d_idle%d_chunk%d", bps, preRoll, chunk), func(t *testing.T) {
					input := make([]float32, preRoll)
					input = append(input, wave...)
					d, err := NewStreamDemodulator(cfg)
					if err != nil {
						t.Fatal(err)
					}
					var bits []bool
					for i := 0; i < len(input); i += chunk {
						end := min(len(input), i+chunk)
						bits = append(bits, d.Feed(input[i:end])...)
					}
					got := bitsToBytes(bits)
					if len(got) < len(payload) {
						t.Fatalf("recovered %d bytes, want >=%d", len(got), len(payload))
					}
					for i := range payload {
						if got[i] != payload[i] {
							t.Fatalf("byte %d: got %02x want %02x", i, got[i], payload[i])
						}
					}
				})
			}
		}
	}
}

func TestStreamModulatorCanRepeatTrainingWithoutResetting(t *testing.T) {
	for _, bitsPerSymbol := range []int{2, 3, 4} {
		mod, err := NewStreamModulator(Config{SampleRate: 48000, SymbolRate: 1600, BitsPerSymbol: bitsPerSymbol, CarrierHz: 1600})
		if err != nil {
			t.Fatal(err)
		}
		first, err := mod.Start()
		if err != nil {
			t.Fatal(err)
		}
		payload, err := mod.WriteBytes([]byte{0x12, 0x34, 0x56, 0x78, 0x9a})
		if err != nil {
			t.Fatal(err)
		}
		training, err := mod.WriteTraining()
		if err != nil {
			t.Fatal(err)
		}
		next, err := mod.WriteBytes([]byte{0xde, 0xad, 0xbe, 0xef})
		if err != nil {
			t.Fatal(err)
		}
		tail, err := mod.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(first) != 96*30 || len(training) < 96*30 || len(payload) == 0 || len(next) == 0 || len(tail) < 0 {
			t.Fatalf("unexpected repeated training dimensions for %d bits/symbol: first=%d payload=%d training=%d next=%d tail=%d", bitsPerSymbol, len(first), len(payload), len(training), len(next), len(tail))
		}
	}
}
