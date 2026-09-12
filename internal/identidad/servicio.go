package identidad

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/BaesBlockchainLabs/logalsend-go/wsdatachannel"

	"pagare/internal/config"
)

// Servicio habla con Logalty para validar identidades.
//
// El cero de Servicio no es utilizable; usa NuevoServicio. Un *Servicio nil
// significa "sin verificación configurada" y todos sus métodos lo dicen con
// ErrDesactivado, para que el resto de la aplicación no tenga que comprobarlo
// en cada llamada.
type Servicio struct {
	cliente *wsdatachannel.Client
	empresa string
	tipo    string
}

// ErrDesactivado se devuelve cuando no hay configuración de Logalty. No es un
// fallo: es el modo en el que corre el desarrollo.
var ErrDesactivado = fmt.Errorf("identidad: la verificación con Logalty no está configurada")

// NuevoServicio construye el servicio a partir de la configuración. Devuelve
// (nil, nil) cuando no hay configuración, que es la forma de decir que la
// verificación está desactivada sin abortar el arranque.
func NuevoServicio(cfg config.LogaltyConfig) (*Servicio, error) {
	if !cfg.Activa() {
		return nil, nil
	}
	// El envío es síncrono y devuelve la URL del usuario en la misma llamada,
	// así que el timeout acota lo que un alta puede llegar a esperar.
	cliente, err := wsdatachannel.NewClient(cfg.Endpoint, cfg.Usuario, cfg.Password,
		&http.Client{Timeout: 60 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("identidad: %w", err)
	}
	return &Servicio{cliente: cliente, empresa: cfg.Empresa, tipo: cfg.Tipo}, nil
}

// Activo indica si hay con quién hablar.
func (s *Servicio) Activo() bool { return s != nil }

// Solicitud es a quién se le pide que valide su identidad.
//
// Referencia es el identificador propio del envío —el id del usuario— y es lo
// que después permite seguirlo: el GUID del portal no está disponible de
// inmediato en los envíos asíncronos.
type Solicitud struct {
	Referencia string
	Nombre     string
	Email      string
	Movil      string
}

// Envio es un envío de validación ya creado.
type Envio struct {
	// GUID identifica el envío en el portal. Viene vacío al crearlo: el portal
	// no lo asigna hasta que el envío sale. Consultar lo resuelve por
	// referencia, que es justo para lo que sirve la referencia.
	GUID       string
	Referencia string
}

// Iniciar crea el envío de validación de identidad. Es el portal quien avisa
// al usuario, por SMS y correo, con el enlace para leer su DNI.
//
// El envío síncrono —el que devolvería el enlace en la misma llamada, para
// llevar al usuario directamente— no sirve aquí: sólo admite tipos de
// contratación, y una validación de documentos de identidad no lo es. El
// portal lo rechaza con el código 122, "el tipo de envío no es contratación".
//
// Se usa la variante multirreceptor aunque haya un solo receptor: la guía de
// integración marca shippingSend como obsoleta y cubre este caso igual.
func (s *Servicio) Iniciar(ctx context.Context, sol Solicitud) (*Envio, error) {
	if s == nil {
		return nil, ErrDesactivado
	}
	if strings.TrimSpace(sol.Movil) == "" {
		return nil, fmt.Errorf("identidad: el tipo de envío de validación exige un móvil")
	}

	// Sin documento: una validación de identidad no firma nada, sólo lee el
	// chip y emite la declaración de atributos.
	//
	// La referencia va en la petición y no en el receptor: es de donde la lee
	// el portal en esta operación, y la del receptor se ignora.
	res, err := s.cliente.ShippingSendMultiReceiver(ctx, wsdatachannel.MultiReceiverSendRequest{
		CompanyID:  s.empresa,
		TypeID:     s.tipo,
		ExternalID: sol.Referencia,
		Receivers: []wsdatachannel.Receiver{{
			ReceiverName:   sol.Nombre,
			ReceiverEmail:  sol.Email,
			ReceiverMobile: sol.Movil,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("identidad: creando el envío: %w", err)
	}
	if len(res) == 0 {
		// El envío puede haberse creado igualmente, así que callar sería lo
		// peor: invitaría a reintentar y crear un segundo.
		return nil, fmt.Errorf("identidad: el portal aceptó la llamada sin devolver resultado; " +
			"comprueba en el portal si el envío se creó antes de reintentar")
	}
	if err := res[0].Err(); err != nil {
		return nil, fmt.Errorf("identidad: el portal rechazó el envío: %w", err)
	}

	envio := &Envio{Referencia: sol.Referencia}
	if docs := res[0].Documents; len(docs) > 0 {
		envio.GUID = docs[0].GUID
	}
	return envio, nil
}

// Situacion es cómo va un envío, en los términos del portal.
type Situacion struct {
	GUID string
	// Terminado indica que el envío ha dejado de moverse, con éxito o sin él.
	Terminado bool
	// Aceptado indica que terminó y el sujeto aceptó. Es condición necesaria
	// para descargar el certificado, pero no suficiente para dar la identidad
	// por buena: eso lo dice Recoger.
	Aceptado bool
	// Descripcion es el estado en palabras del portal, para enseñárselo al
	// usuario y para los logs.
	Descripcion string
}

// Consultar pregunta por el envío de una referencia. Se consulta por
// referencia y no por GUID porque el GUID puede no existir todavía.
func (s *Servicio) Consultar(ctx context.Context, referencia string) (*Situacion, error) {
	if s == nil {
		return nil, ErrDesactivado
	}
	estados, err := s.cliente.ShippingStatusUTC(ctx, wsdatachannel.StatusQuery{
		ExternalIDs: []string{referencia},
	})
	if err != nil {
		return nil, fmt.Errorf("identidad: consultando el envío: %w", err)
	}
	if len(estados) == 0 {
		return nil, fmt.Errorf("identidad: el portal no conoce el envío %q", referencia)
	}

	// Con una referencia por usuario sólo debería haber un envío; si hubiera
	// más de uno tras un reintento, manda el último que se movió.
	e := estados[0]
	for _, otro := range estados[1:] {
		if otro.LastUpdate.After(e.LastUpdate.Time) {
			e = otro
		}
	}

	return &Situacion{
		GUID:        e.GUID,
		Terminado:   e.Settled(),
		Aceptado:    e.Accepted(),
		Descripcion: e.Describe(),
	}, nil
}

// Recoger descarga el certificado XML de la validación y extrae los datos.
//
// Devuelve *ErrNoSuperada cuando el envío terminó pero la verificación no pasó
// las comprobaciones: es un resultado, no una avería, y hay que distinguirlo
// de un fallo de red para no reintentar lo que nunca va a cambiar.
func (s *Servicio) Recoger(ctx context.Context, guid string) (*Datos, error) {
	if s == nil {
		return nil, ErrDesactivado
	}
	if strings.TrimSpace(guid) == "" {
		return nil, fmt.Errorf("identidad: hace falta el GUID del envío para descargar el certificado")
	}

	doc, err := s.cliente.DownloadDocument(ctx,
		wsdatachannel.DocumentIdentificationOCRXML, wsdatachannel.IdentifyByGUID, guid)
	if err != nil {
		return nil, fmt.Errorf("identidad: descargando el certificado: %w", err)
	}
	crudo, err := wsdatachannel.DecodeBinary(doc.Binary)
	if err != nil {
		return nil, fmt.Errorf("identidad: descodificando el certificado: %w", err)
	}
	cert, err := wsdatachannel.ParseIdentityCertificate(crudo)
	if err != nil {
		return nil, fmt.Errorf("identidad: %w", err)
	}
	return DeCertificado(cert)
}
