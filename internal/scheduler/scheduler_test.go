package scheduler

import (
	"testing"
	"time"
)

func mustDate(s string) time.Time {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestClasificar(t *testing.T) {
	now := mustDate("2026-05-25")

	cases := []struct {
		name         string
		tipoVenc     string
		fechaVenc    string
		fechaEmision string
		wantCat      string
	}{
		{"fecha fija en plazo", "fecha_fija", "2026-12-31", "2026-05-01", ""},
		{"fecha fija vencida", "fecha_fija", "2026-05-01", "2026-04-01", CatVencido},
		{"a la vista en plazo", "a_la_vista", "", "2026-01-01", ""},
		{"a la vista caducada (>1 año)", "a_la_vista", "", "2025-01-01", CatCaducadoVista},
		{"prescrito (>3 años) gana a vencido", "fecha_fija", "2023-06-01", "2023-01-01", CatPrescrito},
		{"prescrito exacto a 3 años", "fecha_fija", "2024-01-01", "2023-05-25", CatPrescrito},
		{"sin fecha emision, fecha fija vencida", "fecha_fija", "2026-05-01", "", CatVencido},
		{"emision invalida y en plazo", "fecha_fija", "2027-01-01", "no-fecha", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotCat, msg, _ := Clasificar(c.tipoVenc, c.fechaVenc, c.fechaEmision, now)
			if gotCat != c.wantCat {
				t.Fatalf("Clasificar(%q,%q,%q) categoría = %q, quería %q",
					c.tipoVenc, c.fechaVenc, c.fechaEmision, gotCat, c.wantCat)
			}
			if gotCat != "" && msg == "" {
				t.Fatalf("categoría %q sin mensaje", gotCat)
			}
		})
	}
}

func TestClasificar_PrescritoTienePrioridadSobreCaducadoVista(t *testing.T) {
	now := mustDate("2026-05-25")
	// a la vista emitido hace más de 3 años: ambas reglas aplican, debe ganar PRESCRITO.
	cat, _, art := Clasificar("a_la_vista", "", "2022-01-01", now)
	if cat != CatPrescrito {
		t.Fatalf("categoría = %q, quería %q", cat, CatPrescrito)
	}
	if art != "art. 88 LCCH" {
		t.Fatalf("artículo = %q, quería art. 88 LCCH", art)
	}
}
