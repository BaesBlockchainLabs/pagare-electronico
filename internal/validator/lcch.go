package validator

import (
	"fmt"
	"time"

	"pagare/internal/models"

	"github.com/go-playground/validator/v10"
)

type LCCHValidator struct {
	validate *validator.Validate
}

type ValidationError struct {
	Campo        string `json:"campo"`
	Mensaje      string `json:"mensaje"`
	ArticuloLCCH string `json:"articulo_lcch,omitempty"`
}

type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

func NewLCCHValidator() *LCCHValidator {
	v := validator.New()
	models.RegisterCustomValidations(v)
	return &LCCHValidator{validate: v}
}

func (lv *LCCHValidator) ValidatePagare(p *models.PagareElectronico) *ValidationResult {
	result := &ValidationResult{Valid: true}

	if err := lv.validate.Struct(p); err != nil {
		result.Valid = false
		for _, e := range err.(validator.ValidationErrors) {
			ve := ValidationError{
				Campo:   e.Field(),
				Mensaje: lv.translateError(e),
			}
			ve.ArticuloLCCH = lv.mapToArticle(e.Field())
			result.Errors = append(result.Errors, ve)
		}
	}

	lv.validateLCCHRules(p, result)

	return result
}

func (lv *LCCHValidator) ValidateEndoso(e *models.Endoso) *ValidationResult {
	result := &ValidationResult{Valid: true}

	if e.Tipo != "en_propiedad" && e.Tipo != "en_procuracion" && e.Tipo != "en_blanco" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Tipo", Mensaje: "Tipo de endoso no válido (en_propiedad, en_procuracion, en_blanco)", ArticuloLCCH: "art. 97-100 LCCH",
		})
	}

	lv.validateEndosoLCCH(e, result)

	return result
}

func (lv *LCCHValidator) validateLCCHRules(p *models.PagareElectronico, result *ValidationResult) {
	if p.Denominacion != "PAGARÉ" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Denominacion", Mensaje: "La palabra 'PAGARÉ' debe aparecer en el título", ArticuloLCCH: "art. 94 LCCH",
		})
	}

	if !p.PromesaPago {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "PromesaPago", Mensaje: "La promesa de pago debe ser pura y simple (true) para validez jurídica", ArticuloLCCH: "art. 94 LCCH",
		})
	}

	if p.Importe <= 0 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Importe", Mensaje: "El importe debe ser una cantidad determinada positiva", ArticuloLCCH: "art. 94 LCCH",
		})
	}

	if p.FechaEmision != "" {
		fechaEmision, err := time.Parse("2006-01-02", p.FechaEmision)
		if err == nil {
			lv.validateVencimiento(p, fechaEmision, result)
		}
	}

	if p.Beneficiario.Nombre == "" || p.Beneficiario.NIF == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Beneficiario", Mensaje: "El nombre y NIF del beneficiario son obligatorios", ArticuloLCCH: "art. 94 LCCH",
		})
	}

	if p.Firmante.Nombre == "" || p.Firmante.NIF == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Firmante", Mensaje: "El nombre y NIF del firmante son obligatorios", ArticuloLCCH: "art. 94 LCCH",
		})
	}

	if p.LocalidadEmision == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "LocalidadEmision", Mensaje: "La localidad de emisión es obligatoria", ArticuloLCCH: "art. 94 LCCH",
		})
	}

	if p.LocalidadPago == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "LocalidadPago", Mensaje: "La localidad de pago es obligatoria", ArticuloLCCH: "art. 94 LCCH",
		})
	}

	if p.Aval != nil {
		lv.validateAval(p.Aval, result)
	}
}

func (lv *LCCHValidator) validateVencimiento(p *models.PagareElectronico, fechaEmision time.Time, result *ValidationResult) {
	switch p.Vencimiento.Tipo {
	case "fecha_fija":
		if p.Vencimiento.Fecha == "" {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Campo: "Vencimiento.Fecha", Mensaje: "La fecha de vencimiento es obligatoria para tipo fecha_fija", ArticuloLCCH: "art. 94 LCCH",
			})
			return
		}
		fechaVenc, err := time.Parse("2006-01-02", p.Vencimiento.Fecha)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Campo: "Vencimiento.Fecha", Mensaje: "Formato de fecha inválido (YYYY-MM-DD)", ArticuloLCCH: "art. 94 LCCH",
			})
			return
		}
		if !fechaVenc.After(fechaEmision) {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Campo: "Vencimiento.Fecha", Mensaje: "La fecha de vencimiento debe ser posterior a la fecha de emisión", ArticuloLCCH: "art. 94 LCCH",
			})
		}

	case "a_la_vista":
		unAnio := fechaEmision.AddDate(1, 0, 0)
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Vencimiento", Mensaje: fmt.Sprintf("Pagaré a la vista: plazo máximo de pago hasta %s", unAnio.Format("2006-01-02")), ArticuloLCCH: "art. 39 LCCH",
		})
	}
}

func (lv *LCCHValidator) validateEndosoLCCH(e *models.Endoso, result *ValidationResult) {
	if e.Tipo == "en_propiedad" && e.Endosatario == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Endosatario", Mensaje: "El endoso en propiedad requiere un endosatario", ArticuloLCCH: "art. 97 LCCH",
		})
	}

	if e.Clausula == "no_a_la_orden" && e.Endosatario != nil {
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Clausula", Mensaje: "Endoso 'no a la orden': no se permiten más endosos a partir de este punto", ArticuloLCCH: "art. 99 LCCH",
		})
	}
}

func (lv *LCCHValidator) validateAval(a *models.Aval, result *ValidationResult) {
	if a.Alcance == "parcial" && a.ImporteParcial <= 0 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "ImporteParcial", Mensaje: "El aval parcial requiere un importe positivo", ArticuloLCCH: "art. 101-102 LCCH",
		})
	}
	if a.Avalista.Nombre == "" || a.Avalista.NIF == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Campo: "Avalista", Mensaje: "El nombre y NIF del avalista son obligatorios", ArticuloLCCH: "art. 101-102 LCCH",
		})
	}
}

func (lv *LCCHValidator) IsPrescrito(fechaEmision time.Time) bool {
	plazoPrescripcion := fechaEmision.AddDate(3, 0, 0)
	return time.Now().After(plazoPrescripcion)
}

func (lv *LCCHValidator) translateError(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("El campo %s es obligatorio", e.Field())
	case "oneof":
		return fmt.Sprintf("El campo %s tiene un valor no permitido", e.Field())
	case "gt":
		return fmt.Sprintf("El campo %s debe ser mayor que 0", e.Field())
	case "eq":
		return fmt.Sprintf("El campo %s debe ser true", e.Field())
	case "len":
		return fmt.Sprintf("El campo %s debe tener longitud %s", e.Field(), e.Param())
	case "nif":
		return fmt.Sprintf("El campo %s debe ser un NIF/NIE/CIF válido", e.Field())
	default:
		return fmt.Sprintf("El campo %s no es válido", e.Field())
	}
}

func (lv *LCCHValidator) mapToArticle(field string) string {
	articles := map[string]string{
		"Denominacion":     "art. 94 LCCH",
		"PromesaPago":      "art. 94 LCCH",
		"Importe":          "art. 94 LCCH",
		"Vencimiento":      "art. 94 LCCH",
		"LocalidadPago":    "art. 94 LCCH",
		"Beneficiario":     "art. 94 LCCH",
		"LocalidadEmision": "art. 94 LCCH",
		"Firmante":         "art. 94 LCCH",
		"FechaEmision":     "art. 94 LCCH",
		"Endosatario":      "art. 97 LCCH",
		"Avalista":         "art. 101-102 LCCH",
		"Tipo":             "art. 97-100 LCCH",
	}
	if art, ok := articles[field]; ok {
		return art
	}
	return ""
}
