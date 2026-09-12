// Package identidad valida la identidad de un usuario contra el chip de su DNI
// a través de Logalty, y traduce el certificado XML que sale de esa validación
// a los campos que la plataforma necesita.
//
// El flujo no es síncrono: se crea un envío, se lleva al usuario a la URL que
// devuelve el portal, el usuario lee el chip con su móvil, y sólo entonces hay
// certificado que descargar. Iniciar, Consultar y Recoger son esos tres
// momentos.
package identidad

import (
	"fmt"
	"strings"
	"time"

	"github.com/BaesBlockchainLabs/logalsend-go/wsdatachannel"
)

// Datos son los campos de identidad que el alta toma del certificado, ya
// normalizados. Todo lo que hay aquí es dato personal leído del documento:
// no tiene sitio en un log.
type Datos struct {
	// NIF es el número del documento; Documento y NumeroSoporte describen el
	// documento físico del que se leyó.
	NIF           string
	Documento     string // "DNI", "NIE"...
	NumeroSoporte string // el número de serie de la tarjeta, no el NIF
	Caducidad     string // ISO 8601, aaaa-mm-dd
	Expedicion    string

	Nombre    string
	Apellido1 string
	Apellido2 string
	Sexo      string

	FechaNacimiento string // ISO 8601, aaaa-mm-dd
	LugarNacimiento string
	Nacionalidad    string

	// El domicilio viene en un solo campo, "vía-municipio-provincia", y se
	// reparte aquí. El chip del DNI no lleva código postal, así que ése sigue
	// siendo cosa del usuario.
	Direccion         string // la vía y el número
	Localidad         string
	Provincia         string
	Pais              string // ISO de dos letras, del país emisor del documento
	DomicilioCompleto string // el campo tal como lo da el portal

	// Metodo es cómo se leyó el documento, "NFC" cuando fue del chip.
	Metodo string

	// Los tres campos siguientes son la trazabilidad de la evidencia en
	// Logalty: con ellos se puede volver a pedir el certificado y cotejar la
	// declaración de atributos firmada.
	TokenID         string
	ValidationID    string
	HashDeclaracion string
	Fecha           time.Time
}

// Apellidos une los dos apellidos tal como los lleva el documento.
func (d Datos) Apellidos() string {
	return strings.TrimSpace(strings.Join(omitirVacios(d.Apellido1, d.Apellido2), " "))
}

// NombreCompleto arma el nombre en el orden español.
func (d Datos) NombreCompleto() string {
	return strings.Join(omitirVacios(d.Nombre, d.Apellido1, d.Apellido2), " ")
}

// ErrNoSuperada es el rechazo del propio portal: el envío terminó, pero la
// verificación no se superó. Lleva las comprobaciones bloqueantes que fallaron
// para poder decirle al usuario qué pasó.
type ErrNoSuperada struct {
	Fallos []string
}

func (e *ErrNoSuperada) Error() string {
	if len(e.Fallos) == 0 {
		return "la validación de identidad no se superó"
	}
	return "la validación de identidad no se superó: " + strings.Join(e.Fallos, ", ")
}

// DeCertificado traduce el certificado XML a Datos, rechazando de entrada una
// verificación que no se superó.
//
// Se comprueba Passed y no el estado del envío: un envío llega a su estado
// final tanto si el sujeto se verificó como si no.
func DeCertificado(cert *wsdatachannel.IdentityCertificate) (*Datos, error) {
	if cert == nil {
		return nil, fmt.Errorf("identidad: certificado vacío")
	}
	if !cert.Passed() {
		fallos := make([]string, 0, len(cert.FailedChecks()))
		for _, c := range cert.FailedChecks() {
			fallos = append(fallos, descripcionFallo(c))
		}
		return nil, &ErrNoSuperada{Fallos: fallos}
	}

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
		// NFC_SOD_COUNTRY_CODE es el país que emitió el documento. El domicilio
		// sale de ese mismo documento, así que es el país al que pertenece la
		// dirección; no es una inferencia sobre dónde vive hoy el titular.
		Pais: strings.ToUpper(strings.TrimSpace(a.NFCCountryCode)),

		Metodo: primeroNoVacio(a.Method, cert.Method),

		TokenID:      cert.TokenID,
		ValidationID: cert.ValidationID,
		Fecha:        cert.ResultDate.Time,
	}

	d.Direccion, d.Localidad, d.Provincia = partirDomicilio(d.DomicilioCompleto)

	// SURNAME1/SURNAME2 no siempre vienen; algunos documentos sólo traen los
	// dos apellidos juntos en SURNAME.
	if d.Apellido1 == "" && d.Apellido2 == "" {
		d.Apellido1 = a.Surname
	}

	// El hash de la declaración de atributos es lo que permite después probar
	// que estos datos salieron del chip y no de una foto de la tarjeta.
	for _, art := range cert.Artifacts {
		if art.Kind == "nfc-attributes-declaration" {
			d.HashDeclaracion = art.Hash
			break
		}
	}

	if d.NIF == "" {
		return nil, fmt.Errorf("identidad: el certificado no trae número de documento")
	}
	return d, nil
}

// partirDomicilio reparte el domicilio que da el certificado, que llega en un
// solo campo con la forma "vía-municipio-provincia":
//
//	"POL. NO EL BULL 25-MONOVAR-ALICANTE/ALACANT"
//
// Municipio y provincia se toman de los dos últimos tramos y todo lo anterior
// es la vía, porque el guion también aparece dentro de los nombres de calle
// ("AVDA. RUIZ-PICASSO 3") y es el final lo que tiene forma fija. Un domicilio
// con menos tramos de los esperados se devuelve entero como vía antes que
// repartirlo mal.
//
// La provincia puede venir en las dos lenguas, "ALICANTE/ALACANT", y se deja
// como está: es lo que pone el documento.
func partirDomicilio(domicilio string) (via, localidad, provincia string) {
	tramos := omitirVacios(strings.Split(domicilio, "-")...)
	switch len(tramos) {
	case 0:
		return "", "", ""
	case 1:
		return tramos[0], "", ""
	case 2:
		return tramos[0], tramos[1], ""
	default:
		corte := len(tramos) - 2
		return strings.Join(tramos[:corte], "-"), tramos[corte], tramos[corte+1]
	}
}

// descripcionFallo prefiere el mensaje del portal y cae en el código cuando no
// lo hay, que es lo que se le puede enseñar al usuario.
func descripcionFallo(c wsdatachannel.IdentityCheck) string {
	if m := strings.TrimSpace(c.Message); m != "" {
		return m
	}
	return c.Code
}

func soloFecha(t wsdatachannel.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func primeroNoVacio(valores ...string) string {
	for _, v := range valores {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func omitirVacios(valores ...string) []string {
	out := make([]string, 0, len(valores))
	for _, v := range valores {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
