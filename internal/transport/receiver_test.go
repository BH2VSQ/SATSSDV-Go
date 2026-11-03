package transport

import (
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ccsds"
)

func makeTestSSDVPacket(t *testing.T) []byte {
	t.Helper()
	p := make([]byte, 256)
	p[0], p[1], p[6] = 0x55, 0x66, 3
	binary.BigEndian.PutUint16(p[7:9], 12)
	p[9], p[10], p[11] = 20, 15, 0 // 320x240, Q4, 2x2 MCU mode.
	p[12] = 0xff
	binary.BigEndian.PutUint16(p[13:15], 0xffff)
	for i := 15; i < 220; i++ {
		p[i] = byte(i*5 + 9)
	}
	binary.BigEndian.PutUint32(p[220:224], crc32.ChecksumIEEE(p[1:220]))
	parity, err := fec.RS8Parity(p[1:224])
	if err != nil {
		t.Fatal(err)
	}
	copy(p[224:], parity)
	return p
}

func TestSSDVAX25PacketWaveformRoundTrip(t *testing.T) {
	packet := makeTestSSDVPacket(t)
	enc, err := NewEncoder(Config{Profile: domain.SSDVAX25, Source: ax25.Address{Callsign: "N0CALL"}, Destination: ax25.Address{Callsign: "CQ"}})
	if err != nil {
		t.Fatal(err)
	}
	wave, err := enc.EncodePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeAudio(domain.SSDVAX25, wave)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PacketID != 12 || got[0].ImageID != 3 {
		t.Fatalf("decoded packets: %+v", got)
	}
}

func TestPCSICCSDS1200PacketWaveformRoundTrip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x * y), A: 255})
		}
	}
	pc, err := pcsi.NewEncoder(img, pcsi.Config{Callsign: "N0CALL", ImageID: 9})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := pc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncoder(Config{Profile: domain.PCSICCSDS1200})
	if err != nil {
		t.Fatal(err)
	}
	wave, err := enc.EncodePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeAudio(domain.PCSICCSDS1200, wave)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PacketID != 0 || got[0].ImageID != 9 {
		t.Fatalf("decoded packets: %+v", got)
	}
}

func TestPCSICCSDSBurstUsesOnePreambleAndTrailer(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	pc, err := pcsi.NewEncoder(img, pcsi.Config{Callsign: "N0CALL", ImageID: 11})
	if err != nil {
		t.Fatal(err)
	}
	packets := make([][]byte, 2)
	for i := range packets {
		packets[i], err = pc.EncodePacket(i)
		if err != nil {
			t.Fatal(err)
		}
	}
	enc, err := NewEncoder(Config{Profile: domain.PCSICCSDS1200, AudioSampleRate: 19200})
	if err != nil {
		t.Fatal(err)
	}
	wave, err := enc.EncodePackets(packets)
	if err != nil {
		t.Fatal(err)
	}
	wantSamples := (DefaultCCSDSPreambleBytes + 2*(4+ccsds.PayloadLength) + DefaultCCSDSTrailerBytes) * 8 * (19200 / 1200)
	if len(wave) != wantSamples {
		t.Fatalf("burst samples=%d, want %d (guard sequences should surround the batch once)", len(wave), wantSamples)
	}
	decoded, err := DecodeAudio(domain.PCSICCSDS1200, wave)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0].PacketID != 0 || decoded[1].PacketID != 1 {
		t.Fatalf("decoded burst packets: %+v", decoded)
	}
}

func TestSSDVCCSDS2400GMSKWaveformRoundTrip(t *testing.T) {
	packet := makeTestSSDVPacket(t)
	enc, err := NewEncoder(Config{Profile: domain.SSDVCCSDS2400, AudioSampleRate: 19200})
	if err != nil {
		t.Fatal(err)
	}
	wave, err := enc.EncodePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeAudio(domain.SSDVCCSDS2400, wave)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PacketID != 12 {
		t.Fatalf("decoded 2400 GMSK packets: %+v", got)
	}
}

func TestPCSICCSDS2400PacketWaveformRoundTrip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x*y + y), A: 255})
		}
	}
	pc, err := pcsi.NewEncoder(img, pcsi.Config{Callsign: "N0CALL", ImageID: 19})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := pc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncoder(Config{Profile: domain.PCSICCSDS2400, AudioSampleRate: 19200})
	if err != nil {
		t.Fatal(err)
	}
	wave, err := enc.EncodePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeAudio(domain.PCSICCSDS2400, wave)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PacketID != 0 || got[0].ImageID != 19 {
		t.Fatalf("decoded 2400 PCSI packets: %+v", got)
	}
}

func TestStreamingRXHandlesArbitraryAudioBlockBoundaries(t *testing.T) {
	ssdvPacket := makeTestSSDVPacket(t)
	pcsiImage := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			pcsiImage.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	pcsiEncoder, err := pcsi.NewEncoder(pcsiImage, pcsi.Config{Callsign: "N0CALL", ImageID: 8, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	pcsiPacket, err := pcsiEncoder.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}

	profiles := []struct {
		id           domain.TransportMode
		packet       []byte
		source       ax25.Address
		dest         ax25.Address
		wantPacketID uint16
		wantImageID  uint8
	}{
		{id: domain.SSDVAX25, packet: ssdvPacket, source: ax25.Address{Callsign: "N0CALL"}, dest: ax25.Address{Callsign: "CQ"}, wantPacketID: 12, wantImageID: 3},
		{id: domain.SSDVCCSDS1200, packet: ssdvPacket, wantPacketID: 12, wantImageID: 3},
		{id: domain.SSDVCCSDS2400, packet: ssdvPacket, wantPacketID: 12, wantImageID: 3},
		{id: domain.PCSICCSDS1200, packet: pcsiPacket, wantPacketID: 0, wantImageID: 8},
		{id: domain.PCSICCSDS2400, packet: pcsiPacket, wantPacketID: 0, wantImageID: 8},
	}
	for _, tc := range profiles {
		t.Run(string(tc.id), func(t *testing.T) {
			enc, err := NewEncoder(Config{Profile: tc.id, Source: tc.source, Destination: tc.dest})
			if err != nil {
				t.Fatal(err)
			}
			wave, err := enc.EncodePacket(tc.packet)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := domain.ParseProfile(string(tc.id))
			if err != nil {
				t.Fatal(err)
			}
			wave48 := wave
			if profile.SampleRate != 48000 {
				wave48, err = dsp.ResampleLinear(wave, profile.SampleRate, 48000)
				if err != nil {
					t.Fatal(err)
				}
			}
			decoder, err := NewStreamDecoder(tc.id, 48000)
			if err != nil {
				t.Fatal(err)
			}
			var received []ReceivedPacket
			for offset := 0; offset < len(wave48); {
				end := offset + 317
				if end > len(wave48) {
					end = len(wave48)
				}
				got, feedErr := decoder.Feed(wave48[offset:end])
				if feedErr != nil {
					t.Fatal(feedErr)
				}
				received = append(received, got...)
				offset = end
			}
			if len(received) != 1 || received[0].PacketID != tc.wantPacketID || received[0].ImageID != tc.wantImageID {
				t.Fatalf("stream decoder returned %+v", received)
			}
			// A discontinuity reacquires the modem but cannot re-deliver the
			// same image packet if the capture buffer repeats old audio.
			decoder.Reset()
			var duplicateCount int
			for offset := 0; offset < len(wave48); {
				end := offset + 509
				if end > len(wave48) {
					end = len(wave48)
				}
				got, feedErr := decoder.Feed(wave48[offset:end])
				if feedErr != nil {
					t.Fatal(feedErr)
				}
				duplicateCount += len(got)
				offset = end
			}
			if duplicateCount != 0 {
				t.Fatalf("replayed capture delivered %d duplicate packets", duplicateCount)
			}
		})
	}
}
