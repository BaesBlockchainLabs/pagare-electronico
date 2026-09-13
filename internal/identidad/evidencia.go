package identidad

import (
	"context"
	"fmt"
	"strings"

	"github.com/BaesBlockchainLabs/logalsend-go/wsdatachannel"
)

// Evidencia es el certificado de una validación, entero y descodificado: lo que
// el portal dice que comprobó y con qué resultado.
//
// Es más que los Datos que la plataforma se queda. Aquí están también las
// comprobaciones con su umbral, las puntuaciones biométricas, los
// consentimientos que dio el sujeto y los artefactos firmados —nada de lo cual
// se guarda—, porque para auditar una identidad hace falta ver en qué se apoyó
// y no sólo su conclusión.
//
// Todo esto es dato personal del sujeto. Se sirve a un administrador, en
// pantalla y bajo petición, y no tiene sitio en un log ni en una base de datos.
type Evidencia struct {
	// Resultado es el código de la operación, "0000" en una respuesta correcta,
	// y Motivo su descripción. Describen la petición, no la verificación.
	Resultado string `json:"resultado"`
	Motivo    string `json:"motivo,omitempty"`
	// Superada es el veredicto que importa: el portal dio la identidad por
	// buena y ninguna comprobación bloqueante falló.
	Superada bool `json:"superada"`
	// OCR es el resultado de leer el documento, "OK" cuando pudo.
	OCR string `json:"ocr,omitempty"`

	TokenID      string `json:"token_id,omitempty"`
	ValidationID string `json:"validation_id,omitempty"`
	Estado       string `json:"estado,omitempty"`
	Fecha        string `json:"fecha,omitempty"`

	// Consentimiento, Minimizacion y Metodo describen cómo se identificó el
	// sujeto y qué consintió.
	Consentimiento string `json:"consentimiento,omitempty"`
	Minimizacion   string `json:"minimizacion,omitempty"`
	Metodo         string `json:"metodo,omitempty"`

	// Atributos son los datos leídos del documento, ya repartidos.
	Atributos *Datos `json:"atributos,omitempty"`

	Comprobaciones  []Comprobacion   `json:"comprobaciones,omitempty"`
	Biometria       []Puntuacion     `json:"biometria,omitempty"`
	Consentimientos []Consentimiento `json:"consentimientos,omitempty"`
	Artefactos      []Artefacto      `json:"artefactos,omitempty"`

	// Codigos es todo lo que trae el certificado, incluidos los que este
	// paquete no modela. Es la red de seguridad de una auditoría: si el portal
	// añade algo, se ve igualmente.
	Codigos map[string]string `json:"codigos,omitempty"`
}

// Comprobacion es una fila de la tabla de validaciones del certificado.
type Comprobacion struct {
	Codigo    string `json:"codigo"`
	Resultado string `json:"resultado"`
	Superada  bool   `json:"superada"`
	// Umbral es el filtro que se le aplicó: "1" para una comprobación de sí o
	// no, o la puntuación mínima para una biométrica.
	Umbral string `json:"umbral,omitempty"`
	// Bloqueante indica si fallarla tumba la verificación entera.
	Bloqueante bool   `json:"bloqueante"`
	Mensaje    string `json:"mensaje,omitempty"`
}

// Puntuacion es una medida biométrica. El portal las da como fracciones de uno
// y el certificado en PDF las imprime como porcentajes.
type Puntuacion struct {
	Nombre string  `json:"nombre"`
	Valor  float64 `json:"valor"`
}

// Consentimiento es una respuesta del sujeto al formulario de consentimiento.
// El PDF del certificado no las expone; el XML sí.
type Consentimiento struct {
	ID          string `json:"id"`
	Paso        string `json:"paso,omitempty"`
	Idioma      string `json:"idioma,omitempty"`
	Dado        bool   `json:"dado"`
	Obligatorio bool   `json:"obligatorio"`
}

// Artefacto es un fichero incrustado en el certificado. No se devuelve su
// contenido —pesa y no aporta a una auditoría en pantalla—, sino su huella y si
// cuadra con la que el portal declara.
type Artefacto struct {
	Tipo      string `json:"tipo"`
	Extension string `json:"extension,omitempty"`
	Hash      string `json:"hash,omitempty"`
	Firmado   bool   `json:"firmado"`
	Bytes     int    `json:"bytes"`
	// HashCuadra dice si el contenido responde a la huella declarada, y
	// ProblemaHash por qué no, cuando no.
	HashCuadra   bool   `json:"hash_cuadra"`
	ProblemaHash string `json:"problema_hash,omitempty"`
}

// Evidencia descarga el certificado de una validación y lo devuelve entero.
//
// A diferencia de Recoger, no rechaza una verificación que no se superó: para
// auditar hace falta poder mirar precisamente las que fallaron, y saber por qué.
func (s *Servicio) Evidencia(ctx context.Context, guid string) (*Evidencia, error) {
	if s == nil {
		return nil, ErrDesactivado
	}
	if strings.TrimSpace(guid) == "" {
		return nil, fmt.Errorf("identidad: hace falta el GUID del envío")
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
	return DeCertificadoCompleto(cert), nil
}

// DeCertificadoCompleto traduce el certificado entero, falle o no la
// verificación.
func DeCertificadoCompleto(cert *wsdatachannel.IdentityCertificate) *Evidencia {
	if cert == nil {
		return nil
	}

	e := &Evidencia{
		Resultado:      cert.ResultCode,
		Motivo:         cert.Reason,
		Superada:       cert.Passed(),
		OCR:            cert.OCRResult,
		TokenID:        cert.TokenID,
		ValidationID:   cert.ValidationID,
		Estado:         estadoDelEnvio(cert),
		Consentimiento: cert.ConsentStatus,
		Minimizacion:   cert.DataMinimization,
		Metodo:         cert.Method,
		Codigos:        cert.Raw,
	}
	if !cert.ResultDate.IsZero() {
		e.Fecha = cert.ResultDate.Format("2006-01-02 15:04:05")
	}

	// Los atributos se reutilizan de la traducción normal, pero sin exigir que
	// la verificación se superara.
	if datos, err := DeCertificado(cert); err == nil {
		e.Atributos = datos
	} else {
		atributos := *atributosDe(cert)
		e.Atributos = &atributos
	}

	for _, c := range cert.Checks {
		e.Comprobaciones = append(e.Comprobaciones, Comprobacion{
			Codigo:     c.Code,
			Resultado:  c.Result,
			Superada:   c.Passed(),
			Umbral:     c.Threshold,
			Bloqueante: c.Blocking,
			Mensaje:    c.Message,
		})
	}

	v := cert.Verification
	e.Biometria = []Puntuacion{
		{Nombre: "Correspondencia facial", Valor: v.FaceMatch},
		{Nombre: "Autenticidad del selfi", Valor: v.SelfieAuthenticity},
		{Nombre: "Similitud del selfi", Valor: v.SelfieSimilarity},
		{Nombre: "Prueba de vida", Valor: v.Liveness},
	}

	for _, c := range cert.Consents {
		e.Consentimientos = append(e.Consentimientos, Consentimiento{
			ID: c.ID, Paso: c.Step, Idioma: c.Locale,
			Dado: c.Given, Obligatorio: c.Mandatory,
		})
	}

	for _, a := range cert.Artifacts {
		art := Artefacto{
			Tipo: a.Kind, Extension: a.Extension, Hash: a.Hash, Firmado: a.Signed,
		}
		if datos, err := a.Decode(); err == nil {
			art.Bytes = len(datos)
		}
		if err := a.VerifyHash(); err != nil {
			art.ProblemaHash = err.Error()
		} else {
			art.HashCuadra = true
		}
		e.Artefactos = append(e.Artefactos, art)
	}

	return e
}

// estadoDelEnvio describe en palabras del portal cómo acabó el envío.
func estadoDelEnvio(cert *wsdatachannel.IdentityCertificate) string {
	if cert.ResultComment != "" {
		return fmt.Sprintf("%s (%d)", cert.ResultComment, cert.Result)
	}
	return wsdatachannel.ResultName(cert.Result)
}

// atributosDe reparte los atributos sin pasar por las comprobaciones, para
// poder enseñar los de una verificación que no se superó.
func atributosDe(cert *wsdatachannel.IdentityCertificate) *Datos {
	a := cert.Attributes
	d := &Datos{
		NIF:           strings.ToUpper(strings.TrimSpace(a.IDNumber)),
		Documento:     a.DocumentType,
		NumeroSoporte: a.DocumentNumber,
		Caducidad:     soloFecha(a.ExpiryDate),
		Expedicion:    soloFecha(a.ExpeditionDate),

		Nombre:    a.Name,
		Apellido1: a.Surname1,
		Apellido2: a.Surname2,
		Sexo:      a.Sex,

		FechaNacimiento: soloFecha(a.BirthDate),
		LugarNacimiento: primeroNoVacio(a.BirthMunicipality, a.BirthPlace),
		Nacionalidad:    a.Nationality,

		DomicilioCompleto: strings.TrimSpace(a.StreetAddress),
		Pais:              strings.ToUpper(strings.TrimSpace(a.NFCCountryCode)),
		Metodo:            primeroNoVacio(a.Method, cert.Method),

		TokenID:      cert.TokenID,
		ValidationID: cert.ValidationID,
		Fecha:        cert.ResultDate.Time,
	}
	d.Direccion, d.Localidad, d.Provincia = partirDomicilio(d.DomicilioCompleto)
	if d.Apellido1 == "" && d.Apellido2 == "" {
		d.Apellido1 = a.Surname
	}
	return d
}
