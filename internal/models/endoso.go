package models

// Tipos de endoso y cláusulas conforme a la LCCH (Ley 19/1985), arts. 14-24,
// aplicables al pagaré por remisión del art. 96.
//   - en_propiedad: endoso pleno, transmite todos los derechos (art. 17)
//   - en_procuracion: comisión de cobranza / apoderamiento (art. 21)
//   - en_blanco: sin designar endosatario; al portador equivale (arts. 15-17)
//   - en_garantia: en prenda, "valor en garantía/prenda" (art. 22)
// Cláusulas del endoso:
//   - sin_responsabilidad: "sin mi responsabilidad" / "sin garantía" (art. 18)
//   - no_a_la_orden: prohibición de nuevo endoso por el endosante (art. 18)
//   - sin_gastos: dispensa de protesto, "sin gastos" / "sin protesto" (art. 56)
type Endoso struct {
	Tipo                 string   `json:"tipo" validate:"required,oneof=en_propiedad en_procuracion en_blanco en_garantia"`
	Endosante            Persona  `json:"endosante,omitempty"`
	Endosatario          *Persona `json:"endosatario,omitempty"`
	Fecha                string   `json:"fecha" validate:"required"`
	IdentidadEndosante   string   `json:"identidad_endosante,omitempty"`
	IdentidadEndosatario string   `json:"identidad_endosatario,omitempty"`
	FirmaDigital         string   `json:"firma_digital,omitempty"`
	Clausula             string   `json:"clausula,omitempty" validate:"omitempty,oneof=sin_clausula sin_responsabilidad no_a_la_orden sin_gastos"`
}

type CadenaEndosos struct {
	PagareID      string   `json:"pagare_id"`
	Endosos       []Endoso `json:"endosos"`
	TotalEndosos  int      `json:"total_endosos"`
	TitularActual *Persona `json:"titular_actual,omitempty"`
}

type MetadataEmision struct {
	Action                   string `json:"action"`
	FirmaDigitalPagare       string `json:"firma_digital_pagare,omitempty"`
	FirmaDigitalBeneficiario string `json:"firma_digital_beneficiario,omitempty"`
}

type MetadataEndoso struct {
	Action             string   `json:"action"`
	TipoEndoso         string   `json:"tipo_endoso" validate:"required,oneof=en_propiedad en_procuracion en_blanco en_garantia"`
	Endosatario        *Persona `json:"endosatario,omitempty"`
	FirmaDigitalEndoso string   `json:"firma_digital_endoso,omitempty"`
	Clausula           string   `json:"clausula,omitempty" validate:"omitempty,oneof=sin_clausula sin_responsabilidad no_a_la_orden sin_gastos"`
	Motivo             string   `json:"motivo,omitempty"`
}

type MetadataPago struct {
	Action     string `json:"action" validate:"required,oneof=PAGO ANULACION PRESCRIPCION"`
	FechaPago  string `json:"fecha_pago,omitempty"`
	Referencia string `json:"referencia,omitempty"`
	Motivo     string `json:"motivo,omitempty"`
}
