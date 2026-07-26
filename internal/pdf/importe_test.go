package pdf

import "testing"

func TestImporteEnLetra(t *testing.T) {
	cases := map[float64]string{
		0:         "cero euros",
		1:         "un euro",
		1.5:       "un euro con cincuenta céntimos",
		1.01:      "un euro con un céntimo",
		21:        "veintiún euros",
		100:       "cien euros",
		101:       "ciento un euros",
		1500:      "mil quinientos euros",
		1500.50:   "mil quinientos euros con cincuenta céntimos",
		2000:      "dos mil euros",
		1000000:   "un millón euros",
		2500000:   "dos millones quinientos mil euros",
		999999.99: "novecientos noventa y nueve mil novecientos noventa y nueve euros con noventa y nueve céntimos",
		31:        "treinta y un euros",
		215:       "doscientos quince euros",
	}

	for in, want := range cases {
		if got := importeEnLetra(in); got != want {
			t.Errorf("importeEnLetra(%.2f) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatEUR(t *testing.T) {
	cases := map[float64]string{
		0:       "0,00 €",
		1500.5:  "1.500,50 €",
		1000000: "1.000.000,00 €",
		12.3:    "12,30 €",
	}
	for in, want := range cases {
		if got := formatEUR(in); got != want {
			t.Errorf("formatEUR(%.2f) = %q, want %q", in, got, want)
		}
	}
}
