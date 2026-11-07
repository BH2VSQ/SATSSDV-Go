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
