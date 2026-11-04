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

// pcsiChromaCompression maps the three UI image-size presets to a PCSI MMT
// chroma sampling tradeoff. Values are part of the packet header, so receivers
// can reconstruct them without any out-of-band configuration.
func pcsiChromaCompression(quality string) int {
	switch quality {
	case "low":
		return 80
	case "high":
		return 40
	default:
		return 60
	}
}

// EncodeImage applies the selected Hamstation-compatible image size, encodes
// SSDV or PCSI MMT image packets, wraps the selected link layer and modulates
// one continuous burst. The SSDV JPEG quantizer defaults to level 4; the
// low/medium/high setting controls output dimensions independently.
func EncodeImage(src image.Image, profileID, quality, callsign string, imageID uint8, outputSampleRate int) (Result, error) {
	return EncodeImageWithAX25(src, profileID, quality, callsign, imageID, outputSampleRate, 0, "CQ")
}

// EncodeImageWithAX25 allows the UI to set the local source SSID and the
// destination callsign used by AX.25. The values are ignored by CCSDS modes.
func EncodeImageWithAX25(src image.Image, profileID, quality, callsign string, imageID uint8, outputSampleRate int, sourceSSID uint8, destination string) (Result, error) {
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
		// A larger chroma-compression value assigns more payload bits to Y-only
		// samples, preserving fine luminance detail while reducing packet count.
		// Use a moderate setting for the medium default and progressively favor
		// airtime on low-resolution presets; high resolution retains more color.
		chromaCompression := pcsiChromaCompression(quality)
		encoder, encErr := pcsi.NewEncoder(resized, pcsi.Config{
			Callsign: callsign, ImageID: imageID, BitDepth: 12, ChromaCompression: chromaCompression,
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
		if destination == "" {
			destination = "CQ"
		}
		transportConfig.Source = ax25.Address{Callsign: callsign, SSID: sourceSSID}
		transportConfig.Destination = ax25.Address{Callsign: destination}
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
