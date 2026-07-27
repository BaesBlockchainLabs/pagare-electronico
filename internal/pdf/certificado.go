package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"pagare/internal/models"
)

// Operacion is one entry of the pagaré's life as the certificate tells it.
type Operacion struct {
	Fecha     string
	Tipo      string // EMISION | ENTREGA | ENDOSO | CESION | PAGO | ANULACION | PRESCRIPCION
	Titulo    string // encabezamiento legible
	Detalle   []string
	Articulos string
	Desde     string // clave de quien opera
	Hacia     string // clave de quien recibe, si la hay
}

// Certificador identifies who issues the certificate and answers for it.
type Certificador struct {
	Nombre  string
	Cargo   string
	Entidad string
}

// CertificadoInput carries everything the certificate states.
type CertificadoInput struct {
	P             *models.PagareElectronico
	AssetID       string
	Red           string
	VerifyURL     string
	Estado        string
	FirmantePub   string
	TitularActual string
	Firmado       bool
	Integro       bool
	VerificaMsg   string
	Operaciones   []Operacion
	Certificador  Certificador
	Expedido      time.Time
	Referencia    string
}

// Certificado renders the attestation of a pagaré's content, signature and
// history, in a form meant to be read by someone who has never seen the
// platform: a notary drawing up the protesto, a court, the other party.
//
// It is deliberately verbose where the pagaré itself is terse. The pagaré is a
// title and says what the law requires it to say; the certificate explains what
// each of those mentions is and why it must be there, because its reader is not
// expected to know.
func Certificado(in CertificadoInput) ([]byte, error) {
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(margenCert, 20, margenCert)
	p.SetAutoPageBreak(true, 22)
	registerFonts(p)
	pieDeCertificado(p, in)
	p.AddPage()

	encabezado(p, in)
	seccionIdentificacion(p, in)
	seccionMenciones(p, in)
	seccionVerificacion(p, in)
	seccionHistorico(p, in)
	seccionTitularidad(p, in)
	seccionAlcance(p)
	seccionFirma(p, in)

	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const margenCert = 22.0

func anchoUtil(p *fpdf.Fpdf) float64 {
	w, _ := p.GetPageSize()
	return w - 2*margenCert
}

func pieDeCertificado(p *fpdf.Fpdf, in CertificadoInput) {
	p.SetFooterFunc(func() {
		p.SetY(-16)
		setText(p, colInkSoft)
		p.SetFont(fontFamily, "", 7.5)
		p.CellFormat(anchoUtil(p)/2, 4,
			"Certificado "+in.Referencia, "", 0, "L", false, 0, "")
		p.CellFormat(anchoUtil(p)/2, 4,
			fmt.Sprintf("Página %d", p.PageNo()), "", 0, "R", false, 0, "")
	})
}

func encabezado(p *fpdf.Fpdf, in CertificadoInput) {
	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "", 8.5)
	p.CellFormat(0, 5, strings.ToUpper(in.Certificador.Entidad), "", 1, "L", false, 0, "")

	setText(p, colInk)
	p.SetFont(fontFamily, "B", 22)
	p.CellFormat(0, 12, "Certificado de registro y verificación", "", 1, "L", false, 0, "")

	setText(p, colInkSoft)
	p.SetFont(fontFamily, "I", 10)
	p.MultiCell(anchoUtil(p), 5,
		"de un pagaré electrónico inscrito en libro mayor electrónico", "", "L", false)

	setDraw(p, colGold)
	p.SetLineWidth(0.6)
	y := p.GetY() + 2
	p.Line(margenCert, y, margenCert+anchoUtil(p), y)
	p.SetY(y + 6)
}

// tituloSeccion writes a numbered heading.
func tituloSeccion(p *fpdf.Fpdf, n int, texto string) {
	p.Ln(3)
	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "B", 12)
	p.CellFormat(0, 7, fmt.Sprintf("%d. %s", n, texto), "", 1, "L", false, 0, "")
	setText(p, colInk)
	p.SetFont(fontFamily, "", 10)
}

// dato writes a label/value line, with the article that requires it.
func dato(p *fpdf.Fpdf, etiqueta, valor, articulo string) {
	if valor == "" {
		valor = "—"
	}
	anchoEtiqueta := 52.0
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "", 9)
	p.CellFormat(anchoEtiqueta, 5.5, etiqueta, "", 0, "L", false, 0, "")

	setText(p, colInk)
	p.SetFont(fontFamily, "", 10)
	anchoValor := anchoUtil(p) - anchoEtiqueta - 34
	x := p.GetX()
	y := p.GetY()
	p.MultiCell(anchoValor, 5.5, valor, "", "L", false)
	finY := p.GetY()

	if articulo != "" {
		setText(p, colGoldDeep)
		p.SetFont(fontFamily, "I", 8)
		p.SetXY(x+anchoValor+4, y)
		p.CellFormat(30, 5.5, articulo, "", 0, "L", false, 0, "")
	}
	p.SetY(finY)
}

func parrafo(p *fpdf.Fpdf, texto string) {
	setText(p, colInk)
	p.SetFont(fontFamily, "", 10)
	p.MultiCell(anchoUtil(p), 5.2, texto, "", "J", false)
	p.Ln(1.5)
}

func seccionIdentificacion(p *fpdf.Fpdf, in CertificadoInput) {
	tituloSeccion(p, 1, "Objeto del certificado")
	parrafo(p, fmt.Sprintf(
		"%s, %s de %s, certifica que en el libro mayor electrónico gestionado por esta "+
			"entidad consta inscrito el pagaré electrónico que a continuación se identifica, "+
			"con el contenido, la firma y el histórico de operaciones que se detallan.",
		in.Certificador.Nombre, in.Certificador.Cargo, in.Certificador.Entidad))

	dato(p, "Identificador del activo", in.AssetID, "")
	dato(p, "Red", in.Red, "")
	dato(p, "Estado", estadoLegible(in.Estado), "")
	if in.VerifyURL != "" {
		dato(p, "Verificable en", in.VerifyURL, "")
	}
	dato(p, "Expedido el", in.Expedido.Format("02/01/2006 a las 15:04"), "")
}

func seccionMenciones(p *fpdf.Fpdf, in CertificadoInput) {
	pg := in.P
	tituloSeccion(p, 2, "Contenido del título")
	parrafo(p, "El pagaré recoge las menciones que el artículo 94 de la Ley 19/1985, "+
		"Cambiaria y del Cheque, exige para su validez, indicándose junto a cada una el "+
		"precepto que la impone.")

	dato(p, "Denominación", pg.Denominacion, "art. 94.1")
	promesa := "Promesa pura y simple de pagar " + formatEUR(pg.Importe) +
		" (" + importeEnLetra(pg.Importe) + ")"
	dato(p, "Promesa e importe", promesa, "art. 94.2")

	venc := formatVencimiento(pg.Vencimiento)
	art := "art. 94.3"
	if pg.Vencimiento.Tipo == "a_la_vista" {
		art = "arts. 94.3, 39"
	}
	dato(p, "Vencimiento", venc, art)
	dato(p, "Lugar de pago", pg.LocalidadPago, "art. 94.4")

	benef := strings.TrimSpace(pg.Beneficiario.Nombre + " " + pg.Beneficiario.Apellido)
	if pg.Beneficiario.NIF != "" {
		benef += ", con NIF " + pg.Beneficiario.NIF
	}
	dato(p, "Beneficiario", benef, "art. 94.5")
	dato(p, "Emisión", fmt.Sprintf("%s, a %s", pg.LocalidadEmision, formatFechaLarga(pg.FechaEmision)), "art. 94.6")

	firm := strings.TrimSpace(pg.Firmante.Nombre + " " + pg.Firmante.Apellido)
	if pg.Firmante.NIF != "" {
		firm += ", con NIF " + pg.Firmante.NIF
	}
	if pg.Firmante.EsPersonaJuridica() {
		firm += " (persona jurídica)"
	}
	dato(p, "Firmante", firm, "art. 94.7")

	if r := pg.Firmante.Representante; r != nil {
		rep := strings.TrimSpace(r.Nombre+" "+r.Apellido) + ", " + r.Cargo
		if r.NIF != "" {
			rep += ", con NIF " + r.NIF
		}
		dato(p, "Firmado por poder", rep, "art. 9")
		poder := strings.TrimSpace(r.Acreditacion + " " + r.Referencia)
		if r.Fecha != "" {
			poder = strings.TrimSpace(poder + " de " + formatFechaLarga(r.Fecha))
		}
		if poder == "" {
			poder = "No consta acreditación del poder"
		}
		dato(p, "Poder acreditado mediante", poder, "art. 9")
	}

	if pg.NoALaOrden {
		dato(p, "Cláusula «no a la orden»", "El título no es endosable; sólo cabe transmitirlo por cesión ordinaria", "art. 14")
	}
	for _, c := range pg.Clausulas {
		dato(p, "Cláusula", c, "")
	}
	if av := pg.Aval; av != nil {
		alcance := "total"
		if av.Alcance == "parcial" {
			alcance = "parcial por " + formatEUR(av.ImporteParcial)
		}
		avalista := strings.TrimSpace(av.Avalista.Nombre + " " + av.Avalista.Apellido)
		if av.Avalista.NIF != "" {
			avalista += ", con NIF " + av.Avalista.NIF
		}
		avalado := av.Avalado
		if avalado == "" {
			avalado = "el firmante"
		}
		dato(p, "Aval", fmt.Sprintf("%s, en garantía %s, por %s", avalista, alcance, avalado), "arts. 35-37")
	}
}

func seccionVerificacion(p *fpdf.Fpdf, in CertificadoInput) {
	tituloSeccion(p, 3, "Verificación de la firma")

	if !in.Firmado {
		parrafo(p, "No consta firma del contenido de este pagaré, o la que consta no "+
			"corresponde a la clave que figura en el registro como emisora. "+in.VerificaMsg)
		return
	}

	parrafo(p, "El contenido reseñado en el apartado anterior fue firmado electrónicamente "+
		"por la clave que el propio registro hace constar como creadora del asiento, y que a "+
		"continuación se identifica.")
	dato(p, "Clave del emisor", in.FirmantePub, "")

	if in.Integro {
		parrafo(p, "Comprobado que el contenido que hoy obra en el registro coincide "+
			"exactamente con el que fue firmado en el momento de la emisión: el título no ha "+
			"sido alterado desde entonces.")
	} else {
		parrafo(p, "ADVERTENCIA: el contenido que hoy obra en el registro NO coincide con el "+
			"que fue firmado en la emisión. El título ha sido alterado con posterioridad a su "+
			"firma, y su contenido actual no está amparado por ella.")
	}
}

func seccionHistorico(p *fpdf.Fpdf, in CertificadoInput) {
	tituloSeccion(p, 4, "Histórico de operaciones")
	parrafo(p, "Se relacionan, por orden cronológico, las operaciones asentadas sobre el "+
		"título. El orden es el que el propio libro mayor garantiza.")

	if len(in.Operaciones) == 0 {
		parrafo(p, "No consta ninguna operación.")
		return
	}

	for i, op := range in.Operaciones {
		if p.GetY() > 240 {
			p.AddPage()
		}
		setText(p, colGoldDeep)
		p.SetFont(fontFamily, "B", 10)
		cabecera := fmt.Sprintf("%d. %s", i+1, op.Titulo)
		if op.Fecha != "" {
			cabecera += "  ·  " + formatFechaLarga(op.Fecha)
		}
		p.CellFormat(0, 6, cabecera, "", 1, "L", false, 0, "")

		if op.Articulos != "" {
			setText(p, colInkSoft)
			p.SetFont(fontFamily, "I", 8.5)
			p.CellFormat(0, 4.5, op.Articulos, "", 1, "L", false, 0, "")
		}
		setText(p, colInk)
		p.SetFont(fontFamily, "", 9.5)
		for _, d := range op.Detalle {
			p.SetX(margenCert + 4)
			p.MultiCell(anchoUtil(p)-4, 4.8, "· "+d, "", "L", false)
		}
		p.Ln(2)
	}
}

func seccionTitularidad(p *fpdf.Fpdf, in CertificadoInput) {
	tituloSeccion(p, 5, "Titularidad actual")
	parrafo(p, "Consta como titular del control sobre el registro, que en el modelo de "+
		"documento electrónico transmisible hace las veces de la posesión del título en "+
		"papel, la clave siguiente.")
	dato(p, "Titular", in.TitularActual, "")
}

func seccionAlcance(p *fpdf.Fpdf) {
	tituloSeccion(p, 6, "Alcance y límites de lo certificado")
	parrafo(p, "Este certificado da fe del contenido del registro electrónico y del "+
		"resultado de su verificación. No prejuzga la realidad ni la validez del negocio "+
		"subyacente, ni la solvencia de los obligados.")
	parrafo(p, "El registro se lleva en un libro mayor electrónico NO CUALIFICADO en el "+
		"sentido del artículo 3.52 del Reglamento (UE) 2024/1183: garantiza la integridad de "+
		"los asientos y la exactitud de su orden cronológico, pero no goza de la presunción "+
		"de unicidad que el artículo 45 duodecies, apartado 2, reserva a los libros mayores "+
		"cualificados. En caso de controversia, la unicidad del registro habrá de probarse.")
	parrafo(p, "La verificación de la firma acredita que el contenido es el firmado por una "+
		"clave determinada, no que esa clave corresponda a persona concreta: la identidad "+
		"empleada no es firma electrónica cualificada en el sentido del artículo 25 del "+
		"Reglamento (UE) n.º 910/2014.")
	parrafo(p, "Ni este certificado ni la representación impresa del pagaré constituyen "+
		"título ejecutivo. El acceso al juicio cambiario exige la aportación del documento "+
		"original, conforme a la doctrina de la Sala Primera del Tribunal Supremo (sentencia "+
		"núm. 94/2014, de 5 de marzo).")
}

func seccionFirma(p *fpdf.Fpdf, in CertificadoInput) {
	if p.GetY() > 210 {
		p.AddPage()
	}
	p.Ln(8)
	setDraw(p, colBorder)
	p.SetLineWidth(0.3)
	y := p.GetY()
	p.Line(margenCert, y, margenCert+anchoUtil(p), y)
	p.Ln(6)

	setText(p, colInk)
	p.SetFont(fontFamily, "", 10)
	p.MultiCell(anchoUtil(p), 5.2, fmt.Sprintf(
		"Y para que conste donde convenga, se expide el presente certificado en %s.",
		in.Expedido.Format("02/01/2006")), "", "L", false)
	p.Ln(10)

	p.SetFont(fontFamily, "B", 11)
	p.CellFormat(0, 5.5, in.Certificador.Nombre, "", 1, "L", false, 0, "")
	setText(p, colInkSoft)
	p.SetFont(fontFamily, "", 9.5)
	p.CellFormat(0, 5, in.Certificador.Cargo+" · "+in.Certificador.Entidad, "", 1, "L", false, 0, "")

	p.Ln(4)
	setText(p, colGoldDeep)
	p.SetFont(fontFamily, "I", 8.5)
	p.MultiCell(anchoUtil(p), 4.2,
		"Documento pendiente de firma electrónica cualificada y sello de tiempo "+
			"cualificado. Hasta que se incorporen, su valor probatorio es el de un documento "+
			"privado.", "", "L", false)
}

// estadoLegible turns the internal state into something a reader understands.
func estadoLegible(e string) string {
	switch e {
	case "PAGADO":
		return "Pagado"
	case "ANULADO":
		return "Anulado"
	case "PRESCRITO":
		return "Prescrito"
	case "ENDOSADO":
		return "Endosado"
	case "CEDIDO":
		return "Cedido por cesión ordinaria"
	case "VENCIDO":
		return "Vencido y pendiente de pago"
	case "PENDIENTE_ENTREGA":
		return "Emitido, pendiente de entrega al beneficiario"
	case "":
		return "En vigor"
	default:
		return e
	}
}
