package pdf

import (
	_ "embed"

	"github.com/go-pdf/fpdf"
)

// Fraunces (OFL), la tipografía serif de marca, embebida en el binario para que
// el PDF sea idéntico en cualquier despliegue (sin dependencias externas).
// Subconjunto latino de Google Fonts (incluye € y acentos españoles).

//go:embed fonts/Fraunces-Regular.ttf
var frauncesRegular []byte

//go:embed fonts/Fraunces-Bold.ttf
var frauncesBold []byte

//go:embed fonts/Fraunces-Italic.ttf
var frauncesItalic []byte

//go:embed fonts/Fraunces-BoldItalic.ttf
var frauncesBoldItalic []byte

// fontFamily is the registered family name used throughout the document.
const fontFamily = "Fraunces"

// registerFonts loads the embedded Fraunces faces into the PDF. With UTF-8 fonts
// fpdf accepts raw UTF-8 strings directly (no cp1252 translator needed).
func registerFonts(p *fpdf.Fpdf) {
	p.AddUTF8FontFromBytes(fontFamily, "", frauncesRegular)
	p.AddUTF8FontFromBytes(fontFamily, "B", frauncesBold)
	p.AddUTF8FontFromBytes(fontFamily, "I", frauncesItalic)
	p.AddUTF8FontFromBytes(fontFamily, "BI", frauncesBoldItalic)
}
