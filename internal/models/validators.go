package models

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

var nifRegex = regexp.MustCompile(`^[0-9]{8}[A-Z]$`)
var nieRegex = regexp.MustCompile(`^[XYZ][0-9]{7}[A-Z]$`)
var cifRegex = regexp.MustCompile(`^[A-Z][0-9]{8}$`)

func RegisterCustomValidations(v *validator.Validate) {
	v.RegisterValidation("nif", validateNIF)
}

func validateNIF(fl validator.FieldLevel) bool {
	val := fl.Field().String()
	return nifRegex.MatchString(val) || nieRegex.MatchString(val) || cifRegex.MatchString(val)
}
