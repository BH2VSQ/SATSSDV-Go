# Experimental incremental SSDV-GIF transmission

The Qt 6 native build can transmit one animated GIF image component as an
incremental SSDV stream. This extension reuses the selected SSDV-capable modem
profile (AX.25 UI or CCSDS/M-ary) and the existing audio/PTT path. A normal
static SSDV or PCSI template continues to use the original static-image path.

## Template behavior

- Import a `.gif` into the gallery and add it as a normal visible image layer.
- A template may contain exactly one visible GIF image component.
- Static template layers below the GIF are composited behind every animation
  frame; layers above it are composited in front of every frame.
- The GIF image component's software alpha-contour outline is disabled.
- Select an SSDV TX profile and use the ordinary Send button. No separate GIF
  transmit control is required.
- Repeat transmission reuses the encoded animation waveform and follows the
  normal scheduler: the first send begins immediately, then the configured
  interval countdown starts after each completed burst.

## Encoding and wire format

The sender composes GIF frames (including offsets, transparency and disposal),
resizes them to the selected image-quality preset, and encodes full keyframes
plus sparse MCU restart-segment updates. A full keyframe is forced at least
every 12 frames or earlier when 60% or more of the MCU cells change. The
`SGV1` frame-open (`PacketID=0xfffe`) and frame-close (`PacketID=0xffff`)
control packets carry frame timing, data-packet count, and stream/frame boundary
flags. Each control marker is sent three times for better delimiter reliability.

This is a private SATSSDV-Go extension, not standard SSDV video support. It
uses two reserved high bits in the SSDV flags byte (GIF mode and keyframe), and
its control packets are not understood by unmodified SSDV receivers. Both
transmitting and receiving stations need compatible implementations.

## Safety limits

- GIF input: at most 8 MiB.
- Frames: at most 120.
- Logical screen: at most 8 megapixels.
- Cumulative decoded frame area: at most 64 megapixels.
- Estimated on-air duration: at most 10 minutes per burst.
- PCSI profiles and multiple visible GIF components are not supported.

## Native Qt receive support

The native Qt 6 build also reconstructs the private GIF stream on RX:

- GIF control and data packets are routed to a dedicated GIF session manager,
  never to the static SSDV/PCSI assemblers.
- Repeated frame-open/frame-close control packets are de-duplicated. Keyframes
  initialize the receive canvas; sparse MCU restart segments update only the
  changed image regions in later frames.
- Decodable MCU updates publish progressive PNG previews to the live RX pane.
  Frame-close commits one full-canvas snapshot with the received frame delay.
- The final stream-end marker exports one animated GIF and stores one RX album
  record. The native album preview uses Qt animated-image playback.
- An explicit stream-start marker resets transport duplicate suppression, so
  repeated transmissions that reuse the exact same GIF packets are decoded as
  new RX sessions rather than discarded as duplicates.

Both stations must run a compatible SATSSDV-Go GIF implementation. Ordinary
SSDV receivers can receive the RF waveform but cannot interpret the private
`SGV1` control packets or reconstruct the animation.
