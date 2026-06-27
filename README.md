# Nospero

Command-line printing for Epson LabelWorks LW-600P.

## Setup

Install the CLI:

```sh
go install github.com/oxplot/nospero/cmd/nospero
```

Pair one LW-600P in macOS Bluetooth settings, then run:

```sh
nospero status
```

Nospero discovers the paired printer by Bluetooth Device ID
`vendor=0x0430 product=0x0211` and opens IOBluetooth RFCOMM directly.

For multiple paired printers, select one with `--address`.
The default RFCOMM channel is `1`.

## Usage

```sh
nospero status
nospero diagnose
nospero fonts add Roboto
nospero fonts list
nospero print text "Hello"
nospero print text --font Roboto --font-weight 700 --italic "Hello"
nospero print text --text-align center --font Roboto "Top\nBottom"
nospero print barcode --type code128 "ASSET-42"
nospero print barcode --type qr "https://example.com/asset/42"
nospero print image --file label.png
nospero print mixed --file logo.png --text "Asset 42" --layout left
```

Text printing requires a downloaded local font. `nospero fonts add` accepts a
Google Fonts specimen URL such as `https://fonts.google.com/specimen/Open+Sans`
or a family name such as `Roboto`, then stores a Go-renderable TTF/OTF in the
user cache directory. The default text font is `Roboto`. Text labels accept
`--font-weight 100..900` and `--italic`; if Google Fonts provides a matching
face, Nospero uses it, otherwise it applies the weight or slant in Go during
rendering.

Barcode labels are rendered locally and printed through the same raster path as
text and image labels. Supported barcode types are `aztec`, `codabar`,
`code128`, `code39`, `code93`, `datamatrix`, `ean8`, `ean13`, `pdf417`, `qr`,
`2of5`, and `2of5-interleaved`. Use `--module-dots` when a scanner needs
wider modules than the default.
