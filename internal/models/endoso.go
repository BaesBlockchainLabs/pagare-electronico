package models

type Endoso struct {
	Tipo                 string   `json:"tipo" validate:"required,oneof=en_propiedad en_procuracion en_blanco"`
	Endosante            Persona  `json:"endosante,omitempty"`
	Endosatario          *Persona `json:"endosatario,omitempty"`
	Fecha                string   `json:"fecha" validate:"required"`
	IdentidadEndosante   string   `json:"identidad_endosante,omitempty"`
	IdentidadEndosatario string   `json:"identidad_endosatario,omitempty"`
	FirmaDigital         string   `json:"firma_digital,omitempty"`
	Clausula             string   `json:"clausula,omitempty" validate:"omitempty,oneof=sin_clausula sin_responsabilidad no_a_la_orden"`
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
	TipoEndoso         string   `json:"tipo_endoso" validate:"required,oneof=en_propiedad en_procuracion en_blanco"`
	Endosatario        *Persona `json:"endosatario,omitempty"`
	FirmaDigitalEndoso string   `json:"firma_digital_endoso,omitempty"`
	Clausula           string   `json:"clausula,omitempty" validate:"omitempty,oneof=sin_clausula sin_responsabilidad no_a_la_orden"`
	Motivo             string   `json:"motivo,omitempty"`
}

type MetadataPago struct {
	Action     string `json:"action" validate:"required,oneof=PAGO ANULACION PRESCRIPCION"`
	FechaPago  string `json:"fecha_pago,omitempty"`
	Referencia string `json:"referencia,omitempty"`
	Motivo     string `json:"motivo,omitempty"`
}
