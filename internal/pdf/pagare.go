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
	Fecha        string
	Tipo         string // en_propiedad | en_blanco | en_procuracion | en_garantia
	Endosatario  string
	NIF          string
	Clausula     string
	EndosantePub string // clave pública del endosante (su "firma")
}

// Cesion is a transfer by ordinary assignment of the credit, rendered apart
// from the endorsement chain.
//
// It is kept separate on purpose. The cedente answers for the existence of the
// credit but not for the debtor's solvency (art. 1529 CC), where an endosante
// does answer for payment (art. 18 LCCH); and the debtor keeps against the
// cesionario the defences they held against the cedente. Listing a cesión among
// the endosos would attribute to the cedente a liability they never took on.
type Cesion struct {
	Fecha             string
	Cesionario        string
	NIF               string
	CedentePub        string
	NotificacionFecha string
	NotificacionMedio string
}

// Input carries everything needed to render a pagaré document.
type Input struct {
	P           *models.PagareElectronico
	AssetID     string
	VerifyURL   string // enlace público de verificación (para el QR)
	FirmantePub string // clave pública del firmante (su "firma" en el anverso)
	Estado      string // PAGADO | ANULADO | PRESCRITO | ... (para el sello)
	Endosos     []Endoso
	Cesiones    []Cesion
}

const fontMono = "Courier" // fuente core monoespaciada para las claves públicas

// Paleta del sistema de diseño "Papel Notarial".
var (
	colPaper    = [3]int{250, 246, 238}
	colInk      = [3]int{27, 42, 74}
	colInkSoft  = [3]int{106, 116, 136}
	colGold     = [3]int{194, 155, 60}
	colGoldDeep = [3]int{160, 127, 38}
	colBorder   = [3]int{210, 194, 158}
	colCard     = [3]int{255, 253, 248}
	colChip     = [3]int{246, 239, 217}
)

// Generate renders the full pagaré document as a 2-page PDF: page 1 = anverso
// (front), page 2 = reverso (endorsement chain), the two sides of one sheet.
func Generate(in Input) ([]byte, error) {
	p := fpdf.New("L", "mm", "A4", "")
	p.SetMargins(0, 0, 0)
	p.SetAutoPageBreak(false, 0)
	registerFonts(p)

	p.AddPage()
	renderAnverso(p, in)

	p.AddPage()
	renderReverso(p, in)

	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// frame draws the paper background and the double gold border shared by both sides.
func frame(p *fpdf.Fpdf, w, h float64) {
	setFill(p, colPaper)
	p.Rect(0, 0, w, h, "F")
	setDraw(p, colGold)
	p.SetLineWidth(1.1)
	p.Rect(8, 8, w-16, h-16, "D")
	setDraw(p, colBorder)
	p.SetLineWidth(0.4)
	p.Rect(10.5, 10.5, w-21, h-21, "D")
}

func renderAnverso(p *fpdf.Fpdf, in Input) {
	w, h := p.GetPageSize()
	pg := in.P
	frame(p, w, h)
	mx := 20.0

	// ---- Cabecera ----
	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "", 9)
	p.SetXY(mx, 16)
	p.CellFormat(0, 5, "PAGARÉ ELECTRÓNICO · REGISTRO FEHACIENTE EN BLOCKCHAIN", "", 0, "L", false, 0, "")

	setText(p, colInk)
	p.SetFont(fontFamily, "B", 38)
	p.SetXY(mx, 20)
	p.CellFormat(120, 20, "PAGARÉ", "", 0, "L", false, 0, "")

	// ---- Caja de importe (arriba derecha) ----
	boxW, boxH := 92.0, 22.0
	boxX, boxY := w-mx-boxW, 20.0
	setFill(p, colCard)
	setDraw(p, colGold)
	p.SetLineWidth(0.8)
	p.Rect(boxX, boxY, boxW, boxH, "FD")
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "", 8)
	p.SetXY(boxX+4, boxY+2.5)
	p.CellFormat(boxW-8, 4, "IMPORTE", "", 0, "L", false, 0, "")
	setText(p, colInk)
	p.SetFont(fontFamily, "B", 20)
	p.SetXY(boxX+4, boxY+8)
	p.CellFormat(boxW-8, 10, formatEUR(pg.Importe), "", 0, "R", false, 0, "")

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
	p.SetFont(fontFamily, "", 12.5)
	p.SetXY(mx, 54)
	promesa := fmt.Sprintf(
		"Por el presente PAGARÉ me comprometo a pagar, de forma pura y simple, el día %s, en %s, a %s con NIF %s, %s, la cantidad de:",
		venc, pg.LocalidadPago, benef, pg.Beneficiario.NIF, orden,
	)
	p.MultiCell(w-2*mx, 6.5, promesa, "", "L", false)

	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "BI", 15)
	p.SetX(mx)
	p.MultiCell(w-2*mx, 7, strings.ToUpper(importeEnLetra(pg.Importe)), "", "L", false)

	// ---- Cláusulas / aval (chips) ----
	chipY := p.GetY() + 3
	chipX := mx
	drawChip := func(txt string) {
		p.SetFont(fontFamily, "", 8.5)
		tw := p.GetStringWidth(txt) + 8
		setFill(p, colChip)
		setDraw(p, colGold)
		p.SetLineWidth(0.3)
		p.Rect(chipX, chipY, tw, 6.5, "FD")
		setText(p, colGoldDeep)
		p.SetXY(chipX, chipY)
		p.CellFormat(tw, 6.5, txt, "", 0, "C", false, 0, "")
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
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "", 8.5)
	p.SetXY(mx, baseY)
	p.CellFormat(0, 5, "FIRMANTE (SUSCRIPTOR)", "", 1, "L", false, 0, "")
	setText(p, colInk)
	p.SetFont(fontFamily, "B", 12)
	p.SetX(mx)
	firm := strings.TrimSpace(pg.Firmante.Nombre + " " + pg.Firmante.Apellido)
	p.CellFormat(0, 6, firm+"  ·  NIF "+pg.Firmante.NIF, "", 1, "L", false, 0, "")
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "", 10)
	p.SetX(mx)
	dir := pg.Firmante.DireccionPostal
	p.CellFormat(0, 5.5, strings.TrimSpace(fmt.Sprintf("%s, %s %s (%s)", dir.Direccion, dir.CodigoPostal, dir.Localidad, dir.Pais)), "", 1, "L", false, 0, "")

	setText(p, colInk)
	p.SetFont(fontFamily, "I", 11)
	p.SetXY(mx, baseY+20)
	p.CellFormat(0, 6, fmt.Sprintf("En %s, a %s.", pg.LocalidadEmision, formatFechaLarga(pg.FechaEmision)), "", 1, "L", false, 0, "")

	// Firma electrónica: la clave pública ed25519 del firmante ES la firma.
	sy := baseY + 30
	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "", 8)
	p.SetXY(mx, sy)
	p.CellFormat(0, 4.5, "FIRMA ELECTRÓNICA · CLAVE PÚBLICA (ed25519)", "", 1, "L", false, 0, "")
	if in.FirmantePub != "" {
		setText(p, colInk)
		p.SetFont(fontMono, "B", 10)
		p.SetXY(mx, sy+5)
		p.CellFormat(0, 5.5, in.FirmantePub, "", 1, "L", false, 0, "")
	} else {
		setDraw(p, colInk)
		p.SetLineWidth(0.3)
		p.Line(mx, sy+9.5, mx+80, sy+9.5)
	}
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "I", 8.5)
	p.SetXY(mx, sy+11.5)
	p.CellFormat(0, 4.5, "Firmado digitalmente y registrado de forma fehaciente en blockchain.", "", 0, "L", false, 0, "")

	// QR de verificación (abajo derecha)
	drawQR(p, in, w-mx-30, baseY+6, 30)

	// Sello de estado (pagaré cerrado) por encima de todo.
	if isClosedEstado(in.Estado) {
		drawEstadoSello(p, w, h, in.Estado)
	}
}

// isClosedEstado reports whether the pagaré is no longer live (paid/void/expired
// by prescription), which warrants the red cancellation overprint.
func isClosedEstado(e string) bool {
	switch e {
	case "PAGADO", "ANULADO", "PRESCRITO":
		return true
	}
	return false
}

// drawEstadoSello overprints the document with a red cancellation mark: two
// diagonal bars (top and bottom) and the state word across the centre, all
// semi-transparent so the underlying data stays legible.
func drawEstadoSello(p *fpdf.Fpdf, w, h float64, estado string) {
	red := [3]int{198, 42, 42}

	p.SetAlpha(0.30, "Normal")
	setDraw(p, red)
	p.SetLineCapStyle("round")
	p.SetLineWidth(7)
	// Barras ascendentes (paralelas al texto rotado +20°).
	p.Line(20, 74, w-20, 52)     // barra superior
	p.Line(20, h-52, w-20, h-74) // barra inferior
	p.SetLineCapStyle("butt")

	p.SetAlpha(0.38, "Normal")
	setText(p, red)
	p.SetFont(fontFamily, "B", 88)
	cx, cy := w/2, h/2
	p.TransformBegin()
	p.TransformRotate(20, cx, cy)
	tw := p.GetStringWidth(estado)
	p.SetXY(cx-tw/2, cy-20)
	p.CellFormat(tw, 40, estado, "", 0, "C", false, 0, "")
	p.TransformEnd()

	p.SetAlpha(1, "Normal")
}

func renderReverso(p *fpdf.Fpdf, in Input) {
	w, h := p.GetPageSize()
	frame(p, w, h)
	mx := 20.0

	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "", 9)
	p.SetXY(mx, 16)
	p.CellFormat(0, 5, "REVERSO · ENDOSOS, AVALES Y CLÁUSULAS", "", 0, "L", false, 0, "")
	setText(p, colInk)
	p.SetFont(fontFamily, "B", 24)
	p.SetXY(mx, 20)
	p.CellFormat(0, 12, "Cadena de endosos", "", 0, "L", false, 0, "")
	setDraw(p, colBorder)
	p.SetLineWidth(0.3)
	p.Line(mx, 38, w-mx, 38)

	y := 44.0
	if len(in.Endosos) == 0 {
		setText(p, colInkSoft)
		p.SetFont(fontFamily, "I", 11)
		p.SetXY(mx, y)
		p.CellFormat(0, 6, "Sin endosos. El pagaré permanece en poder del primer tenedor.", "", 1, "L", false, 0, "")
		y += 12
	} else {
		for i, e := range in.Endosos {
			y = drawEndoso(p, mx, y, w-2*mx, i+1, e)
			if y > h-40 { // salto de seguridad si hubiera muchísimos
				break
			}
		}
	}

	// Cesiones, aparte de la cadena: no son endosos ni comprometen al cedente
	// con la solvencia del deudor.
	y = drawCesiones(p, mx, y, w-2*mx, h, in.Cesiones)

	// Espacios en blanco para endosos manuales (como el dorso físico).
	drawEndosoEnBlanco(p, mx, &y, w-2*mx, h)

	// Pie: nota legal
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "I", 8)
	p.SetXY(mx, h-18)
	nota := "El endoso debe ser firmado por el endosante (arts. 16-17 y 96 LCCH). La legitimación del tenedor resulta de una serie no interrumpida de endosos (art. 19). El endoso en blanco se perfecciona con la sola firma."
	if len(in.Cesiones) > 0 {
		nota += " La cesión ordinaria no es endoso: el cedente responde de la existencia del crédito pero no de la solvencia del deudor (art. 1529 CC), y ha de notificarse al deudor para serle oponible (art. 1527 CC)."
	}
	p.MultiCell(w-2*mx, 4, nota, "", "L", false)

	if isClosedEstado(in.Estado) {
		drawEstadoSello(p, w, h, in.Estado)
	}
}

// drawCesiones renders the assignments under a heading of their own, so the
// reverse never reads as if the cedente had endorsed. Returns the new Y.
func drawCesiones(p *fpdf.Fpdf, x, y, wdt, h float64, cesiones []Cesion) float64 {
	if len(cesiones) == 0 {
		return y
	}

	y += 4
	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "B", 12)
	p.SetXY(x, y)
	p.CellFormat(0, 6, "Cesiones ordinarias (arts. 347-348 CCom)", "", 1, "L", false, 0, "")
	y += 9

	for i, c := range cesiones {
		if y > h-46 {
			break
		}
		blockH := 24.0
		setFill(p, colCard)
		setDraw(p, colBorder)
		p.SetLineWidth(0.3)
		p.Rect(x, y, wdt, blockH, "FD")

		setText(p, colInk)
		p.SetFont(fontFamily, "B", 10)
		p.SetXY(x+5, y+3)
		p.CellFormat(0, 5, fmt.Sprintf("%d. Cedido a %s", i+1, c.Cesionario), "", 1, "L", false, 0, "")

		setText(p, colInkSoft)
		p.SetFont(fontFamily, "", 8)
		p.SetXY(x+5, y+9)
		detalle := c.NIF
		if c.Fecha != "" {
			detalle = strings.TrimSpace(detalle + "  ·  " + formatFechaLarga(c.Fecha))
		}
		p.CellFormat(0, 4, detalle, "", 1, "L", false, 0, "")

		p.SetXY(x+5, y+14)
		aviso := "Pendiente de notificación al deudor (art. 1527 CC)"
		if c.NotificacionFecha != "" {
			aviso = "Notificada al deudor el " + formatFechaLarga(c.NotificacionFecha)
			if c.NotificacionMedio != "" {
				aviso += " por " + c.NotificacionMedio
			}
		}
		p.CellFormat(0, 4, aviso, "", 1, "L", false, 0, "")

		if c.CedentePub != "" {
			p.SetFont(fontMono, "", 7)
			p.SetXY(x+5, y+18)
			p.CellFormat(0, 4, "cedente "+shortID(c.CedentePub), "", 1, "L", false, 0, "")
		}

		y += blockH + 4
	}
	return y
}

// drawEndoso renders one endorsement block and returns the new Y.
func drawEndoso(p *fpdf.Fpdf, x, y, wdt float64, n int, e Endoso) float64 {
	blockH := 28.0
	setFill(p, colCard)
	setDraw(p, colBorder)
	p.SetLineWidth(0.3)
	p.Rect(x, y, wdt, blockH, "FD")

	// Nº de orden (círculo dorado)
	setFill(p, colChip)
	setDraw(p, colGold)
	p.Circle(x+7, y+7, 4, "FD")
	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "B", 9)
	p.SetXY(x+3, y+4.2)
	p.CellFormat(8, 6, strconv.Itoa(n), "", 0, "C", false, 0, "")

	setText(p, colInk)
	p.SetFont(fontFamily, "B", 11)
	p.SetXY(x+16, y+3)
	p.CellFormat(wdt-22, 6, formulaEndoso(e), "", 0, "L", false, 0, "")

	setText(p, colInkSoft)
	p.SetFont(fontFamily, "", 9)
	p.SetXY(x+16, y+10)
	meta := tipoEndosoLabel(e.Tipo)
	if e.Fecha != "" {
		meta += "  ·  " + formatFechaLarga(e.Fecha)
	}
	if e.Clausula != "" {
		meta += "  ·  " + e.Clausula
	}
	p.CellFormat(wdt-22, 5, meta, "", 0, "L", false, 0, "")

	// Firma del endosante = su clave pública completa, en su propia línea.
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "I", 8)
	p.SetXY(x+16, y+16.5)
	p.CellFormat(wdt-22, 4, "Firma del endosante (clave pública ed25519):", "", 0, "L", false, 0, "")
	firma := "—"
	if e.EndosantePub != "" {
		firma = e.EndosantePub
	}
	setText(p, colInk)
	p.SetFont(fontMono, "B", 9)
	p.SetXY(x+16, y+20.5)
	p.CellFormat(wdt-22, 5, firma, "", 0, "L", false, 0, "")

	return y + blockH + 4
}

// drawEndosoEnBlanco fills the remaining space with ruled blank endorsement slots.
func drawEndosoEnBlanco(p *fpdf.Fpdf, x float64, y *float64, wdt, h float64) {
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "", 8)
	limit := h - 24
	first := true
	for *y+22 < limit {
		if first {
			p.SetXY(x, *y-1)
			p.CellFormat(0, 4, "Espacio para endosos manuales:", "", 1, "L", false, 0, "")
			*y += 5
			first = false
		}
		setDraw(p, colBorder)
		p.SetLineWidth(0.25)
		p.Rect(x, *y, wdt, 18, "D")
		*y += 22
	}
}

func drawQR(p *fpdf.Fpdf, in Input, x, y, size float64) {
	if in.VerifyURL == "" {
		return
	}
	png, err := qrcode.Encode(in.VerifyURL, qrcode.Medium, 256)
	if err != nil {
		return
	}
	opt := fpdf.ImageOptions{ImageType: "PNG"}
	p.RegisterImageOptionsReader("qr", opt, bytes.NewReader(png))
	p.ImageOptions("qr", x, y, size, size, false, opt, 0, "")
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "", 7.5)
	p.SetXY(x-30, y+size+1)
	p.CellFormat(size+30, 4, "Verificable en blockchain", "", 1, "R", false, 0, "")
	if in.AssetID != "" {
		p.SetX(x - 30)
		p.CellFormat(size+30, 4, "ID "+shortID(in.AssetID), "", 0, "R", false, 0, "")
	}
}

// ---------- fórmulas de endoso ----------

func formulaEndoso(e Endoso) string {
	dest := strings.TrimSpace(e.Endosatario)
	if e.NIF != "" {
		dest += " (NIF " + e.NIF + ")"
	}
	switch e.Tipo {
	case "en_blanco":
		return "Endoso en blanco (a la orden del portador)"
	case "en_procuracion":
		return "Páguese a " + dest + " — valor al cobro"
	case "en_garantia":
		return "Páguese a " + dest + " — valor en garantía"
	default: // en_propiedad
		if dest == "" {
			return "Endoso en blanco"
		}
		return "Páguese a la orden de " + dest
	}
}

func tipoEndosoLabel(t string) string {
	switch t {
	case "en_blanco":
		return "Endoso en blanco (art. 15)"
	case "en_procuracion":
		return "Endoso en procuración (art. 21)"
	case "en_garantia":
		return "Endoso en garantía (art. 22)"
	default:
		return "Endoso en propiedad (art. 17)"
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
	// ASCII "..." (no ellipsis glyph): safe in the core monospace font too.
	return id[:8] + "..." + id[len(id)-6:]
}
