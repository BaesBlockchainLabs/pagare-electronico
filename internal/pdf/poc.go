// Package pdf renders pagarés to PDF. This file is the proof-of-concept for the
// library decision (pagare-7nv): it confirms github.com/go-pdf/fpdf can render
// the Spanish text and the euro sign the pagaré needs, in pure Go.
//
// Encoding note: fpdf's built-in core fonts (Times/Helvetica) speak cp1252,
// which already contains the euro sign (€) and every accented character used in
// Spanish. UnicodeTranslatorFromDescriptor("cp1252") maps our UTF-8 strings to
// that encoding, so no external font file is required for a correct PoC. The
// final "bonito" version (pagare-szz) may embed a serif TTF (e.g. Fraunces).
package pdf

import (
	"bytes"

	"github.com/go-pdf/fpdf"
)

// renderPoC builds a tiny one-page PDF exercising the euro sign and Spanish
// accents, returning the raw PDF bytes.
func renderPoC() ([]byte, error) {
	p := fpdf.New("P", "mm", "A4", "")
	tr := p.UnicodeTranslatorFromDescriptor("cp1252")
	p.AddPage()

	p.SetFont("Times", "B", 20)
	p.CellFormat(0, 12, tr("PAGARÉ"), "", 1, "C", false, 0, "")

	p.SetFont("Times", "", 12)
	lines := []string{
		"Importe: 1.500,00 € (mil quinientos euros)",
		"Acción cambiaria, endoso, prescripción y aval.",
		"Firmante: José Muñoz Peña — NIF 12345678Z",
		"Localidad de emisión: A Coruña",
	}
	for _, l := range lines {
		p.CellFormat(0, 8, tr(l), "", 1, "L", false, 0, "")
	}

	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
