package models

type PagareElectronico struct {
	IDPagare         string      `json:"id_pagare" validate:"required"`
	Denominacion     string      `json:"denominacion" validate:"required,oneof=PAGARÉ"`
	PromesaPago      bool        `json:"promesa_pago" validate:"required,eq=true"`
	Importe          float64     `json:"importe" validate:"required,gt=0"`
	Moneda           string      `json:"moneda" validate:"required,oneof=EUR"`
	Vencimiento      Vencimiento `json:"vencimiento" validate:"required"`
	LocalidadPago    string      `json:"localidad_pago" validate:"required"`
	Beneficiario     Persona     `json:"beneficiario" validate:"required"`
	LocalidadEmision string      `json:"localidad_emision" validate:"required"`
	FechaEmision     string      `json:"fecha_emision" validate:"required"`
	Firmante         Firmante    `json:"firmante" validate:"required"`
	Aval             *Aval       `json:"aval,omitempty"`
	Clausulas        []string    `json:"clausulas,omitempty"`
	// NoALaOrden: cláusula "no a la orden" puesta por el librador en la emisión
	// (art. 14 LCCH). Priva al pagaré de su condición de título endosable; solo
	// podrá transmitirse por cesión ordinaria, no por endoso.
	NoALaOrden bool `json:"no_a_la_orden,omitempty"`
}

type Vencimiento struct {
	Tipo  string `json:"tipo" validate:"required,oneof=fecha_fija a_la_vista"`
	Fecha string `json:"fecha,omitempty" validate:"omitempty,datetime=2006-01-02"`
}

type Persona struct {
	Nombre   string `json:"nombre" validate:"required"`
	Apellido string `json:"apellido,omitempty"`
	NIF      string `json:"nif" validate:"required,nif"`
}

// Firmante is whoever undertakes to pay: the obligado cambiario.
//
// Nombre and NIF always name that obligado, which keeps the persona física case
// unchanged. When the pagaré is issued by a company, Nombre carries the razón
// social and NIF the CIF, and Representante names the natural person who signs
// for it — because a company cannot sign, only its representative can.
type Firmante struct {
	// Tipo: fisica (por defecto) | juridica.
	Tipo                string          `json:"tipo,omitempty" validate:"omitempty,oneof=fisica juridica"`
	Nombre              string          `json:"nombre" validate:"required"`
	Apellido            string          `json:"apellido,omitempty"`
	NIF                 string          `json:"nif" validate:"required,nif"`
	DireccionPostal     DireccionPostal `json:"direccion_postal" validate:"required"`
	Representante       *Representante  `json:"representante,omitempty"`
	IdentidadBlockchain *IdentidadBC    `json:"identidad_blockchain,omitempty"`
	FirmaDigital        string          `json:"firma_digital,omitempty"`
}

// EsPersonaJuridica reports whether the pagaré is issued by a company.
func (f *Firmante) EsPersonaJuridica() bool { return f.Tipo == "juridica" }

// Representante is the natural person signing on behalf of a company, and the
// record of the power they rely on.
//
// The art. 9 LCCH requirement is not paperwork: whoever signs for another must
// hold a poder and must say so clearly in the antefirma, and whoever signs
// without one is bound personally by the title. So the cargo has to appear on
// the document, and the absence of an acreditación is worth warning about.
//
// The acreditación is deliberately thin — a free-text kind, a reference and a
// date — because it stands in for a verifiable credential of registral origin
// that the European ecosystem is still building, and no structure invented here
// is likely to match the one that eventually arrives, if it does.
type Representante struct {
	Nombre   string `json:"nombre" validate:"required"`
	Apellido string `json:"apellido,omitempty"`
	NIF      string `json:"nif" validate:"required,nif"`
	// Cargo: administrador único, apoderado, consejero delegado… Debe constar
	// en la antefirma (art. 9 LCCH).
	Cargo string `json:"cargo" validate:"required"`
	// Acreditacion, Referencia y Fecha: constancia del poder por medios
	// convencionales (nota simple, copia autorizada de escritura…). La
	// comprobación ocurre fuera de la plataforma; aquí solo se registra.
	Acreditacion string `json:"acreditacion,omitempty"`
	Referencia   string `json:"referencia,omitempty"`
	Fecha        string `json:"fecha,omitempty"`
}

type DireccionPostal struct {
	Direccion    string `json:"direccion" validate:"required"`
	Localidad    string `json:"localidad" validate:"required"`
	CodigoPostal string `json:"codigo_postal" validate:"required"`
	Region       string `json:"region,omitempty"`
	Pais         string `json:"pais" validate:"required,len=2"`
}

// Aval del pagaré (arts. 35-37 LCCH, aplicables por el art. 96). Un tercero
// (avalista) garantiza el pago, total o parcialmente.
type Aval struct {
	Avalista       Persona `json:"avalista" validate:"required"`
	Alcance        string  `json:"alcance" validate:"required,oneof=total parcial"`
	ImporteParcial float64 `json:"importe_parcial,omitempty"`
	// Avalado: persona a quien se avala (art. 36). A falta de indicación se
	// entiende avalado el firmante del pagaré.
	Avalado string `json:"avalado,omitempty"`
}
