// Package tximage prepares quality-scaled source images and turns supported
// image codecs into a transport waveform suitable for the full-duplex TX path.
package tximage

import (
	"image"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/imagequality"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

type Result struct {
	Image       *image.RGBA
	Waveform    []float32
	PacketCount int
	SampleRate  int
}

// EncodeImage applies the selected Hamstation-compatible image size, encodes
// SSDV or PCSI MMT image packets, wraps the selected link layer and modulates
// one continuous burst. The SSDV JPEG quantizer defaults to level 4; the
// low/medium/high setting controls output dimensions independently.
func EncodeImage(src image.Image, profileID, quality, callsign string, imageID uint8, outputSampleRate int) (Result, error) {
	var result Result
	profile, err := domain.ParseProfile(profileID)
	if err != nil {
		return result, err
	}
	resized, err := imagequality.ResizeCover(src, imagequality.Preset(quality))
	if err != nil {
		return result, err
	}
	var packets [][]byte
	switch profile.ImageCodec {
	case "SSDV":
		packets, err = ssdv.EncodeImage(resized, ssdv.EncodeConfig{
			Callsign: callsign, ImageID: imageID, Quality: ssdv.DefaultJPEGQuality, FEC: true,
		})
	case "PCSI-MMT":
		encoder, encErr := pcsi.NewEncoder(resized, pcsi.Config{
			Callsign: callsign, ImageID: imageID, BitDepth: 12, ChromaCompression: 20,
		})
		if encErr != nil {
			return result, encErr
		}
		packets = make([][]byte, 0, encoder.PacketCount())
		for i := 0; i < encoder.PacketCount(); i++ {
			packet, encErr := encoder.EncodePacket(i)
			if encErr != nil {
				return result, encErr
			}
			packets = append(packets, packet)
		}
	default:
		return result, domain.ErrCodecNotImplemented
	}
	if err != nil {
		return result, err
	}
	transportConfig := transport.Config{
		Profile: domain.TransportMode(profileID), AudioSampleRate: outputSampleRate,
	}
	if profile.LinkLayer == "AX.25 UI / HDLC" {
		transportConfig.Source = ax25.Address{Callsign: callsign}
		transportConfig.Destination = ax25.Address{Callsign: "CQ"}
	}
	transportEncoder, err := transport.NewEncoder(transportConfig)
	if err != nil {
		return result, err
	}
	waveform, err := transportEncoder.EncodePackets(packets)
	if err != nil {
		return result, err
	}
	rate, err := transportEncoder.SampleRate()
	if err != nil {
		return result, err
	}
	return Result{Image: resized, Waveform: waveform, PacketCount: len(packets), SampleRate: rate}, nil
}
