// Package firma hace que una persona firme un PDF a través de Logalty, con el
// servicio de contratación, y recoge el PDF firmado.
//
// Lo que devuelve no es una firma cualquiera: el PDF firmado lleva una firma
// PAdES y un sello de tiempo RFC 3161, emitidos bajo una CA cualificada. Es
// decir, es la firma que el art. 25 eIDAS equipara a la manuscrita, que la
// firma ed25519 de la cadena no da por sí sola.
//
// El flujo es de tres pasos: se crea el envío, el portal avisa al firmante por
// correo y SMS con el enlace donde firma, y cuando termina hay PDF firmado que
// descargar.
//
// El envío síncrono —el que devuelve una URL de firma en la misma llamada, para
// llevar al firmante directamente— no sirve aquí, y eso costó averiguarlo: crea
// el envío, el portal estampa el documento y devuelve una URL con toda la
// pinta de ser buena, pero abrirla redirige al formulario de acceso del portal
// en lugar de abrir la firma, incluso recién generada y en una ventana limpia.
// Comprobado también con buildSamlUrl, que devuelve una URL de la misma forma y
// se comporta igual. Con el envío asíncrono, el enlace que manda el portal sí
// abre la firma: es el camino que llegó a Finalizada / Aceptada con su PDF
// firmado.
package firma

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/BaesBlockchainLabs/logalsend-go/wsdatachannel"

	"pagare/internal/config"
)

// ErrDesactivada se devuelve cuando no hay configuración de firma. No es un
// fallo: es el modo en el que corre el desarrollo.
var ErrDesactivada = fmt.Errorf("firma: la firma de PDF con Logalty no está configurada")

// Servicio manda PDFs a firmar.
//
// Un *Servicio nil significa "sin firma configurada" y todos sus métodos lo
// dicen con ErrDesactivada, para que el resto de la aplicación no tenga que
// comprobarlo en cada llamada.
type Servicio struct {
	cliente *wsdatachannel.Client
	empresa string
	tipo    string
	// remitente es el "Enviado por" del aviso que recibe el firmante. Vacío,
	// el portal pone el nombre de la empresa.
	remitente string
}

// NuevoServicio construye el servicio a partir de la configuración. Devuelve
// (nil, nil) cuando no hay tipo de contratación configurado, que es la forma de
// decir que la firma está desactivada sin abortar el arranque.
func NuevoServicio(cfg config.LogaltyConfig) (*Servicio, error) {
	if !cfg.FirmaActiva() {
		return nil, nil
	}
	cliente, err := wsdatachannel.NewClient(cfg.Endpoint, cfg.Usuario, cfg.Password,
		&http.Client{Timeout: 120 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("firma: %w", err)
	}
	return &Servicio{
		cliente:   cliente,
		empresa:   cfg.Empresa,
		tipo:      cfg.TipoContrato,
		remitente: cfg.Remitente,
	}, nil
}

// Activa indica si hay con quién firmar.
func (s *Servicio) Activa() bool { return s != nil }

// Firmante es quien tiene que firmar. El tipo de envío electrónico exige del
// portal los cinco campos, así que faltar uno es un rechazo, no un aviso.
type Firmante struct {
	Nombre    string
	Apellidos string
	NIF       string
	Email     string
	Movil     string
}

func (f Firmante) falta() string {
	switch {
	case strings.TrimSpace(f.Nombre) == "":
		return "nombre"
	case strings.TrimSpace(f.NIF) == "":
		return "NIF"
	case strings.TrimSpace(f.Email) == "":
		return "email"
	case strings.TrimSpace(f.Movil) == "":
		return "móvil"
	}
	return ""
}

// Peticion es un PDF para que lo firme una persona.
type Peticion struct {
	// Referencia es nuestro identificador del envío, con el que se sigue
	// después. El GUID del portal no siempre está disponible de inmediato.
	Referencia string
	Firmante   Firmante
	// Fichero es el nombre que ve el firmante, con extensión.
	Fichero string
	PDF     []byte
	// Asunto es el asunto del correo con que el portal avisa al firmante.
	Asunto string
}

// Envio es un envío de firma ya creado.
type Envio struct {
	// GUID identifica el envío en el portal. Viene vacío al crearlo: el portal
	// no lo asigna hasta que el envío sale. Consultar lo resuelve por
	// referencia, que es para lo que sirve la referencia.
	GUID       string
	Referencia string
	// HashOriginal es el SHA-256 del PDF que se mandó. Hay que conservarlo:
	// Recoger lo usa para comprobar que se firmó esto y no otra cosa.
	HashOriginal string
}

// Iniciar crea el envío de firma. Es el portal quien avisa al firmante, por
// correo y SMS, con el enlace donde firma.
//
// Se usa la variante multirreceptor porque las de un solo receptor están
// obsoletas, y la asíncrona porque la síncrona devuelve una URL que no abre la
// firma; ver la documentación del paquete. El tipo tiene que ser de
// contratación: cualquier otro se rechaza con el código 122.
func (s *Servicio) Iniciar(ctx context.Context, p Peticion) (*Envio, error) {
	if s == nil {
		return nil, ErrDesactivada
	}
	if falta := p.Firmante.falta(); falta != "" {
		return nil, fmt.Errorf("firma: al firmante le falta el %s, que el portal exige", falta)
	}
	if len(p.PDF) == 0 {
		return nil, fmt.Errorf("firma: no hay PDF que firmar")
	}
	if EsAcroForm(p.PDF) {
		// El portal añade su propio AcroForm con el campo de firma, así que
		// rechaza los que ya traen uno. Mejor decirlo aquí que comerse un
		// rechazo remoto que no explica esto.
		return nil, fmt.Errorf("firma: el PDF es un AcroForm y el portal no los acepta")
	}

	hash := sha256Hex(p.PDF)
	fichero := p.Fichero
	if fichero == "" {
		fichero = "documento.pdf"
	}

	res, err := s.cliente.ShippingSendMultiReceiver(ctx,
		wsdatachannel.MultiReceiverSendRequest{
			CompanyID:  s.empresa,
			TypeID:     s.tipo,
			ExternalID: p.Referencia,
			SenderName: s.remitente,
			Subject:    p.Asunto,
			Receivers: []wsdatachannel.Receiver{{
				ReceiverName:         p.Firmante.Nombre,
				ReceiverLastName1:    p.Firmante.Apellidos,
				ReceiverIdentityID:   p.Firmante.NIF,
				ReceiverIdentityType: "NIF",
				ReceiverEmail:        p.Firmante.Email,
				ReceiverMobile:       p.Firmante.Movil,
			}},
			Files: []wsdatachannel.BinaryContentItem{{
				// El tipo MIME, no la extensión, aunque el comentario del campo
				// en el SDK diga lo contrario: es lo que declaraba el envío que
				// llegó a firmarse, y lo que manda su propia herramienta.
				Type:    "application/pdf",
				Name:    fichero,
				Content: wsdatachannel.EncodeContent(p.PDF),
			}},
		})
	if err != nil {
		return nil, fmt.Errorf("firma: creando el envío: %w", err)
	}
	if len(res) == 0 {
		// El envío puede haberse creado igualmente, así que callar sería lo
		// peor: invitaría a reintentar y poner a la misma persona a firmar dos
		// veces el mismo documento.
		return nil, fmt.Errorf("firma: el portal aceptó la llamada sin devolver resultado; " +
			"comprueba en el portal si el envío se creó antes de reintentar")
	}
	if err := res[0].Err(); err != nil {
		return nil, fmt.Errorf("firma: el portal rechazó el envío: %w", err)
	}

	envio := &Envio{Referencia: p.Referencia, HashOriginal: hash}
	if docs := res[0].Documents; len(docs) > 0 {
		envio.GUID = docs[0].GUID
	}
	return envio, nil
}

// Situacion es cómo va un envío de firma, en los términos del portal.
type Situacion struct {
	GUID string
	// Terminado indica que el envío ha dejado de moverse, con firma o sin ella.
	Terminado bool
	// Firmado indica que terminó y el firmante aceptó, que es cuando hay PDF
	// firmado que descargar.
	Firmado     bool
	Descripcion string
}

// Consultar pregunta por el envío de una referencia.
func (s *Servicio) Consultar(ctx context.Context, referencia string) (*Situacion, error) {
	if s == nil {
		return nil, ErrDesactivada
	}
	estados, err := s.cliente.ShippingStatusUTC(ctx, wsdatachannel.StatusQuery{
		ExternalIDs: []string{referencia},
	})
	if err != nil {
		return nil, fmt.Errorf("firma: consultando el envío: %w", err)
	}
	if len(estados) == 0 {
		return nil, fmt.Errorf("firma: el portal no conoce el envío %q", referencia)
	}

	e := estados[0]
	for _, otro := range estados[1:] {
		if otro.LastUpdate.After(e.LastUpdate.Time) {
			e = otro
		}
	}
	return &Situacion{
		GUID:        e.GUID,
		Terminado:   e.Settled(),
		Firmado:     e.Accepted(),
		Descripcion: e.Describe(),
	}, nil
}

// Firmado es el PDF firmado y lo que hace falta para citarlo como prueba.
type Firmado struct {
	// PDF es el documento firmado: lleva la firma PAdES del portal y un sello
	// de tiempo RFC 3161.
	PDF []byte
	// Hash es el SHA-256 del PDF firmado, y HashOriginal el del que se mandó a
	// firmar. Los dos se anotan: el segundo es lo que ata la firma al
	// contenido que se generó.
	Hash         string
	HashOriginal string
}

// Recoger descarga el PDF firmado y comprueba que se firmó lo que se mandó.
//
// La comprobación no es decorativa: el portal devuelve en DocumentOriginal el
// fichero que subimos, byte a byte, así que cotejarlo contra nuestro hash es
// la manera barata de probar que la firma cubre el pagaré que generamos y no
// otro documento. Si no cuadra, es un error: guardar esa firma sería peor que
// no tener ninguna.
func (s *Servicio) Recoger(ctx context.Context, guid, hashOriginal string) (*Firmado, error) {
	if s == nil {
		return nil, ErrDesactivada
	}
	if strings.TrimSpace(guid) == "" {
		return nil, fmt.Errorf("firma: hace falta el GUID del envío para descargar el PDF firmado")
	}

	original, err := s.descargar(ctx, guid, wsdatachannel.DocumentOriginal)
	if err != nil {
		return nil, err
	}
	if hashOriginal != "" {
		if visto := sha256Hex(original); visto != hashOriginal {
			return nil, fmt.Errorf("firma: el portal firmó otro documento: se mandó %s y devuelve %s",
				hashOriginal, visto)
		}
	}

	firmado, err := s.descargar(ctx, guid, wsdatachannel.DocumentSigned)
	if err != nil {
		return nil, err
	}
	if len(firmado) == 0 {
		return nil, fmt.Errorf("firma: el portal no devolvió PDF firmado para el envío %s", guid)
	}

	return &Firmado{
		PDF:          firmado,
		Hash:         sha256Hex(firmado),
		HashOriginal: sha256Hex(original),
	}, nil
}

func (s *Servicio) descargar(ctx context.Context, guid string, cual wsdatachannel.DocumentType) ([]byte, error) {
	doc, err := s.cliente.DownloadDocument(ctx, cual, wsdatachannel.IdentifyByGUID, guid)
	if err != nil {
		return nil, fmt.Errorf("firma: descargando %s: %w", cual, err)
	}
	datos, err := wsdatachannel.DecodeBinary(doc.Binary)
	if err != nil {
		return nil, fmt.Errorf("firma: descodificando %s: %w", cual, err)
	}
	return datos, nil
}

// EsAcroForm indica si el PDF trae un formulario, que es lo que el portal
// rechaza porque añade el suyo con el campo de firma.
func EsAcroForm(pdf []byte) bool {
	return bytes.Contains(pdf, []byte("/AcroForm"))
}

func sha256Hex(datos []byte) string {
	suma := sha256.Sum256(datos)
	return hex.EncodeToString(suma[:])
}
