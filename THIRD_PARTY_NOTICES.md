# Third-party notices and source provenance

## ARCSSD CCSDS 2400 modem

The 2400-bit/s pulse-shaping transmitter and quadrature/timing-recovery receiver in:

- `internal/modem/ccsds/gmsk_tx.go`
- `internal/modem/ccsds/gmsk_rx.go`

are Go translations of algorithms and coefficient sequences in the ARCSSD CCSDS source archive supplied for this project. The original C source carries the following notice:

> Copyright 2015 WEI Mingchuan, BG2BHC and HIT Research Center of Satellite Technology. This software is free software; you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3, or (at your option) any later version.

The translated files are marked `GPL-3.0-or-later`. Review the GPL obligations for the combined application before distributing binaries or source. The original C implementation is not included in this repository.

## SSDV protocol reference

The SSDV packet field layout and abbreviated-JPEG behavior are implemented from the public protocol description and compatibility observations. No upstream `fsphil/ssdv` C source is included or copied into this repository. That upstream project is GPL-3.0-or-later; any future source port or vendoring must preserve its notices and licensing obligations.

## Reed-Solomon

The project's `internal/fec/rs8.go` is a native Go implementation. It does not include the C RS source from the supplied ARCSSD or SSDV archives.

## PCSI MMT reference archive

The supplied PCSI archive contains the MMT transmitter, packet decoder and `pcsiolw.py` reference, but the uploaded archive does not contain a LICENSE/COPYING file or a source-level license notice. The SATSSDV-Go implementation is an independent Go implementation of the documented packet layout, DCT sampling path and OWL-QN-style objective, cross-checked against numerical reference behavior. The upstream license and permission to redistribute derived implementation details must be confirmed with the original author before public distribution. The archive's use of the Python `lbfgs` package includes an explicit MIT attribution for that dependency, but this does not establish a license for the entire PCSI archive.
