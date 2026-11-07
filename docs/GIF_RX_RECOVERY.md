# GIF RX Recovery and Partial Album Checkpoints

## Keyframe-loss behavior

The RX manager treats a failed or incomplete keyframe as a recoverable stream
condition, not as a terminal decoder error. It keeps the best available canvas
(or a neutral placeholder before the first usable keyframe), continues applying
independently decodable delta-frame MCU updates, and remains in
`awaitingKeyframe` state until a later keyframe has both:

- received all data packets advertised by the frame-open control packet; and
- decoded the expected MCU coverage.

A failed periodic keyframe does not discard the previous canvas or prevent
subsequent delta frames from being decoded. A later complete periodic keyframe
re-establishes the reference canvas. The album item remains marked `partial`
when any frame in the stream was lost or incomplete, even if later frames
successfully resynchronize.

## Packet accounting

The number of unique frame data packets is captured before the temporary packet
map is released for decoding. Completion checks must not inspect the copied
frame's packet map after that map has been cleared. This also prevents complete
GIF transfers from being incorrectly labelled as partial.

## Partial GIF persistence

The RX manager marks a session dirty whenever a decodable progressive PNG
snapshot or a completed frame becomes available. If no more GIF packets arrive
for two seconds, it emits a `GIFPartialArchive` checkpoint. The application
exports the current animation snapshot (including the latest progressive
frame, when available) and inserts or updates one album record with status
`partial`.

A final stream-end marker triggers an immediate final export and updates the
same album record to `complete` only when no frame was lost or abandoned. This
idle-checkpoint policy avoids re-encoding and rewriting the entire growing GIF
after every frame, which would otherwise cause quadratic work for long
animations. If an archive write fails, the dirty session is retained and the
checkpoint is retried after the next idle interval.

## Limits and semantics

- A partial album GIF contains only frames that could be decoded or a latest
  progressive preview that could be encoded as PNG. Undecodable data is never
  fabricated into a frame.
- If no frame or progressive preview can be decoded, there is no valid GIF
  image to store yet; the manager continues waiting for later data/keyframes.
- Partial records remain marked `partial`, even after later keyframe recovery,
  because one or more earlier animation frames are missing or incomplete.
- The RX session remains active after partial checkpoints, so later packets can
  update the same album item rather than creating duplicate entries.

## Continued decoding after arbitrary packet loss

The incremental RX path now decodes one bounded MCU entropy interval at a time.
An interval is considered decodable only when every physical SSDV packet in
that interval is present and its restart boundaries are known. A missing packet
invalidates that interval only; it does not close the animation stream, clear
the frame canvas, or stop later frame IDs from entering the manager.

The receiver retains the final EOI packet ID for each recently committed frame.
If an 8+2/late-FEC packet arrives after the frame-close marker, the receiver
retries the interval from the last MCU restart point through EOI. MCU coverage
is advanced by the number of blocks actually returned by the sparse decoder,
not by the theoretical range up to the end of the image. This avoids falsely
marking a truncated final entropy tail as a complete keyframe.

The SSDV sparse preview decoder also caches its immutable 16x16 JPEG template
and parsed header using `sync.Once`. Previously this small template was encoded
and parsed for every incremental MCU interval. The RX manager queue is bounded
at 8192 decoded packets (up from 512), and GIF preview PNG/Base64 publication is
throttled independently from packet processing. The manager remains
non-blocking; `DroppedPackets()` remains the authoritative signal of queue
overload.

Regression coverage includes:

- A high-entropy 640x480 keyframe with a missing packet in the middle of an
  MCU entropy segment, followed by decoding of later MCU regions and late-FEC
  repair of the missing packet.
- A 25-frame animation where one delta-frame packet and one periodic-keyframe
  packet are lost. All subsequent delta frames must still be processed, and the
  next complete periodic keyframe must restore keyframe synchronization.
- An asynchronous high-entropy GIF burst exceeding the old 512-packet queue
  capacity; all packets must be consumed and `DroppedPackets()` must remain 0.
