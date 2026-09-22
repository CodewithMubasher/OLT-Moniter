// Package qr renders QR codes as compact terminal text using braille characters.
package qr

import (
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// quietZone is how many empty modules we keep around the code on each side.
// go-qrcode's Bitmap() ships a spec-standard 4-module quiet zone, which is
// wider than a terminal needs once the code already sits inside a bordered
// box; 2 modules is still enough margin for scanners to lock on, and cuts
// visible size noticeably without hurting scannability.
const quietZone = 2

// Render returns the QR code using a 2×4 braille cell for each terminal
// character. This preserves the approximate square shape of QR modules in a
// typical tall terminal cell while making the linking screen about half as
// wide and half as tall as the old half-block rendering. Light modules are
// drawn as the foreground dots on the QR screen's dark background.
func Render(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "", err
	}
	full := q.Bitmap() // [row][col], true = dark, includes go-qrcode's own quiet-zone border

	// go-qrcode's default border is 4 modules; trim it down to quietZone on
	// every side so the rendered code is as small as it can be while still
	// scanning reliably.
	const libraryBorder = 4
	trim := libraryBorder - quietZone
	if trim < 0 {
		trim = 0
	}
	bm := full
	if trim > 0 && len(full) > 2*trim {
		bm = make([][]bool, len(full)-2*trim)
		for i := range bm {
			row := full[i+trim]
			if len(row) > 2*trim {
				bm[i] = row[trim : len(row)-trim]
			} else {
				bm[i] = row
			}
		}
	}

	get := func(r, c int) bool {
		if r < 0 || r >= len(bm) || c < 0 || c >= len(bm[r]) {
			return false // outside = light
		}
		return bm[r][c]
	}

	var sb strings.Builder
	for r := 0; r < len(bm); r += 4 {
		for c := 0; c < len(bm[0]); c += 2 {
			// Unicode braille dot positions for a 2×4 cell. A filled dot is
			// a light QR module; an empty dot remains the dark module.
			bits := 0
			for dr := 0; dr < 4; dr++ {
				for dc := 0; dc < 2; dc++ {
					if !get(r+dr, c+dc) {
						bits |= 1 << brailleBit(dr, dc)
					}
				}
			}
			sb.WriteRune(rune(0x2800 + bits))
		}
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func brailleBit(row, col int) int {
	// Braille dots are ordered 1,2,3,7 down the left and 4,5,6,8 down
	// the right; their zero-based Unicode bit indexes are below.
	return [4][2]int{{0, 3}, {1, 4}, {2, 5}, {6, 7}}[row][col]
}
