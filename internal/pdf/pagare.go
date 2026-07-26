package pdf

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	qrcode "github.com/skip2/go-qrcode"

	"pagare/internal/models"
)

// Endoso is one link of the endorsement chain, rendered on the reverse.
type Endoso struct {
	Fecha       string
	Tipo        string
	Endosatario string
	NIF         string
	Clausula    string
}

// Input carries everything needed to render a pagaré document.
type Input struct {
	P         *models.PagareElectronico
	AssetID   string
	VerifyURL string // enlace público de verificación (para el QR)
	Endosos   []Endoso
}

// Paleta del sistema de diseño "Papel Notarial".
var (
	colPaper    = [3]int{250, 246, 238}
	colInk      = [3]int{27, 42, 74}
	colInkSoft  = [3]int{106, 116, 136}
	colGold     = [3]int{194, 155, 60}
	colGoldDeep = [3]int{160, 127, 38}
	colBorder   = [3]int{210, 194, 158}
)

// Generate renders the anverso (page 1) of the pagaré. The reverso (endorsement
// chain) is added by a later iteration.
func Generate(in Input) ([]byte, error) {
	p := fpdf.New("L", "mm", "A4", "")
	p.SetMargins(0, 0, 0)
	p.SetAutoPageBreak(false, 0)
	tr := p.UnicodeTranslatorFromDescriptor("cp1252")

	p.AddPage()
	renderAnverso(p, tr, in)

	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderAnverso(p *fpdf.Fpdf, tr func(string) string, in Input) {
	w, h := p.GetPageSize()
	pg := in.P

	// Fondo papel + doble filete dorado (aire de certificado).
	setFill(p, colPaper)
	p.Rect(0, 0, w, h, "F")
	setDraw(p, colGold)
	p.SetLineWidth(1.1)
	p.Rect(8, 8, w-16, h-16, "D")
	setDraw(p, colBorder)
	p.SetLineWidth(0.4)
	p.Rect(10.5, 10.5, w-21, h-21, "D")

	mx := 20.0 // margen interior de contenido

	// ---- Cabecera ----
	setText(p, colGoldDeep)
	p.SetFont("Times", "", 9)
	p.SetXY(mx, 16)
	p.CellFormat(0, 5, tr("PAGARÉ ELECTRÓNICO · REGISTRO FEHACIENTE EN BLOCKCHAIN"), "", 0, "L", false, 0, "")

	setText(p, colInk)
	p.SetFont("Times", "B", 40)
	p.SetXY(mx, 20)
	p.CellFormat(120, 20, tr("PAGARÉ"), "", 0, "L", false, 0, "")

	// ---- Caja de importe (arriba derecha) ----
	boxW, boxH := 92.0, 22.0
	boxX, boxY := w-mx-boxW, 20.0
	setFill(p, [3]int{255, 253, 248})
	setDraw(p, colGold)
	p.SetLineWidth(0.8)
	p.Rect(boxX, boxY, boxW, boxH, "FD")
	setText(p, colInkSoft)
	p.SetFont("Times", "", 8)
	p.SetXY(boxX+4, boxY+2.5)
	p.CellFormat(boxW-8, 4, tr("IMPORTE"), "", 0, "L", false, 0, "")
	setText(p, colInk)
	p.SetFont("Times", "B", 22)
	p.SetXY(boxX+4, boxY+7.5)
	p.CellFormat(boxW-8, 11, tr(formatEUR(pg.Importe)), "", 0, "R", false, 0, "")

	// Filete bajo la cabecera
	setDraw(p, colBorder)
	p.SetLineWidth(0.3)
	p.Line(mx, 46, w-mx, 46)

	// ---- Cuerpo: promesa de pago ----
	venc := formatVencimiento(pg.Vencimiento)
	benef := strings.TrimSpace(pg.Beneficiario.Nombre + " " + pg.Beneficiario.Apellido)
	orden := "o a su orden"
	if pg.NoALaOrden {
		orden = "(no a la orden)"
	}

	setText(p, colInk)
	p.SetFont("Times", "", 13)
	p.SetXY(mx, 54)
	promesa := fmt.Sprintf(
		"Por el presente PAGARÉ me comprometo a pagar, de forma pura y simple, el día %s, en %s, a %s con NIF %s, %s, la cantidad de:",
		venc, pg.LocalidadPago, benef, pg.Beneficiario.NIF, orden,
	)
	p.MultiCell(w-2*mx, 6.5, tr(promesa), "", "L", false)

	// Importe en letra, destacado.
	setText(p, colGoldDeep)
	p.SetFont("Times", "BI", 15)
	p.SetX(mx)
	letra := strings.ToUpper(importeEnLetra(pg.Importe))
	p.MultiCell(w-2*mx, 7, tr("# "+letra+" #"), "", "L", false)

	// ---- Cláusulas / aval (chips) ----
	chipY := p.GetY() + 3
	chipX := mx
	drawChip := func(txt string) {
		p.SetFont("Times", "", 8.5)
		tw := p.GetStringWidth(tr(txt)) + 8
		setFill(p, [3]int{246, 239, 217})
		setDraw(p, colGold)
		p.SetLineWidth(0.3)
		p.Rect(chipX, chipY, tw, 6.5, "FD")
		setText(p, colGoldDeep)
		p.SetXY(chipX, chipY)
		p.CellFormat(tw, 6.5, tr(txt), "", 0, "C", false, 0, "")
		chipX += tw + 4
	}
	if pg.NoALaOrden {
		drawChip("Cláusula «no a la orden» (art. 14 LCCH)")
	}
	for _, c := range pg.Clausulas {
		drawChip(c)
	}
	if pg.Aval != nil {
		av := pg.Aval
		avTxt := "Con aval de " + strings.TrimSpace(av.Avalista.Nombre+" "+av.Avalista.Apellido)
		if av.Alcance == "parcial" {
			avTxt += fmt.Sprintf(" (parcial: %s)", formatEUR(av.ImporteParcial))
		}
		drawChip(avTxt)
	}

	// ---- Pie: firmante + emisión (izquierda) y QR (derecha) ----
	baseY := h - 62

	// Firmante
	setText(p, colInkSoft)
	p.SetFont("Times", "", 8.5)
	p.SetXY(mx, baseY)
	p.CellFormat(0, 5, tr("FIRMANTE (SUSCRIPTOR)"), "", 1, "L", false, 0, "")
	setText(p, colInk)
	p.SetFont("Times", "B", 12)
	p.SetX(mx)
	firm := strings.TrimSpace(pg.Firmante.Nombre + " " + pg.Firmante.Apellido)
	p.CellFormat(0, 6, tr(firm+"  ·  NIF "+pg.Firmante.NIF), "", 1, "L", false, 0, "")
	setText(p, colInkSoft)
	p.SetFont("Times", "", 10)
	p.SetX(mx)
	dir := pg.Firmante.DireccionPostal
	dirTxt := strings.TrimSpace(fmt.Sprintf("%s, %s %s (%s)", dir.Direccion, dir.CodigoPostal, dir.Localidad, dir.Pais))
	p.CellFormat(0, 5.5, tr(dirTxt), "", 1, "L", false, 0, "")

	// Emisión + firma
	setText(p, colInk)
	p.SetFont("Times", "I", 11)
	p.SetXY(mx, baseY+20)
	p.CellFormat(0, 6, tr(fmt.Sprintf("En %s, a %s.", pg.LocalidadEmision, formatFechaLarga(pg.FechaEmision))), "", 1, "L", false, 0, "")

	setDraw(p, colInk)
	p.SetLineWidth(0.3)
	p.Line(mx, baseY+38, mx+70, baseY+38)
	setText(p, colInkSoft)
	p.SetFont("Times", "", 9)
	p.SetXY(mx, baseY+39)
	p.CellFormat(70, 5, tr("Firma del firmante"), "", 0, "C", false, 0, "")

	// QR de verificación (abajo derecha)
	if in.VerifyURL != "" {
		if png, err := qrcode.Encode(in.VerifyURL, qrcode.Medium, 256); err == nil {
			opt := fpdf.ImageOptions{ImageType: "PNG"}
			p.RegisterImageOptionsReader("qr", opt, bytes.NewReader(png))
			qrSize := 30.0
			qrX := w - mx - qrSize
			qrY := baseY + 6
			p.ImageOptions("qr", qrX, qrY, qrSize, qrSize, false, opt, 0, "")
			setText(p, colInkSoft)
			p.SetFont("Times", "", 7.5)
			p.SetXY(qrX-30, qrY+qrSize+1)
			p.CellFormat(qrSize+30, 4, tr("Verificable en blockchain"), "", 1, "R", false, 0, "")
			if in.AssetID != "" {
				p.SetX(qrX - 30)
				p.CellFormat(qrSize+30, 4, tr("ID "+shortID(in.AssetID)), "", 0, "R", false, 0, "")
			}
		}
	}
}

// ---------- helpers de formato ----------

func setFill(p *fpdf.Fpdf, c [3]int) { p.SetFillColor(c[0], c[1], c[2]) }
func setDraw(p *fpdf.Fpdf, c [3]int) { p.SetDrawColor(c[0], c[1], c[2]) }
func setText(p *fpdf.Fpdf, c [3]int) { p.SetTextColor(c[0], c[1], c[2]) }

// formatEUR formatea 1500 -> "1.500,00 €" (miles con punto, decimales con coma).
func formatEUR(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	cents := int64(math.Round(v * 100))
	entero := cents / 100
	dec := cents % 100
	s := strconv.FormatInt(entero, 10)
	var out []byte
	for i, d := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, d)
	}
	res := fmt.Sprintf("%s,%02d €", string(out), dec)
	if neg {
		res = "-" + res
	}
	return res
}

var mesesES = []string{"", "enero", "febrero", "marzo", "abril", "mayo", "junio",
	"julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}

// formatFechaLarga "2026-07-26" -> "26 de julio de 2026".
func formatFechaLarga(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d de %s de %d", t.Day(), mesesES[int(t.Month())], t.Year())
}

func formatVencimiento(v models.Vencimiento) string {
	if v.Tipo == "a_la_vista" {
		return "a la vista"
	}
	return formatFechaLarga(v.Fecha)
}

func shortID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:8] + "…" + id[len(id)-6:]
}
