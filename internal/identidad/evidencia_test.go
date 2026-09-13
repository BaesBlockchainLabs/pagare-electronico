package identidad

import (
	"os"
	"strings"
	"testing"

	"github.com/BaesBlockchainLabs/logalsend-go/wsdatachannel"
)

// La evidencia enseña en qué se apoyó la verificación, no sólo su conclusión.
func TestDeCertificadoCompleto(t *testing.T) {
	e := DeCertificadoCompleto(certificado(t, "testdata/certificado-dni.xml"))
	if e == nil {
		t.Fatal("sin evidencia")
	}

	if !e.Superada {
		t.Error("el certificado de muestra se superó")
	}
	if e.Resultado != "0000" {
		t.Errorf("resultado = %q", e.Resultado)
	}
	if e.TokenID == "" || e.Fecha == "" {
		t.Errorf("falta trazabilidad: token=%q fecha=%q", e.TokenID, e.Fecha)
	}
	if e.Atributos == nil || e.Atributos.NIF != "00000000T" {
		t.Errorf("atributos = %+v", e.Atributos)
	}

	// Las tres comprobaciones del fixture, con su umbral y si son bloqueantes.
	if len(e.Comprobaciones) != 3 {
		t.Fatalf("comprobaciones = %d, se esperaban 3", len(e.Comprobaciones))
	}
	var bloqueantes, fallidas int
	for _, c := range e.Comprobaciones {
		if c.Codigo == "" {
			t.Error("una comprobación sin código no dice nada")
		}
		if c.Bloqueante {
			bloqueantes++
		}
		if !c.Superada {
			fallidas++
		}
	}
	if bloqueantes != 2 {
		t.Errorf("bloqueantes = %d, se esperaban 2", bloqueantes)
	}
	// TEST_NFC_CHIP_PRESENCE viene en KO pero no bloquea, así que la
	// verificación se superó igual: eso es justo lo que hay que poder ver.
	if fallidas != 1 {
		t.Errorf("fallidas = %d, se esperaba 1 no bloqueante", fallidas)
	}

	if len(e.Biometria) != 4 {
		t.Errorf("biometría = %d medidas", len(e.Biometria))
	}
	if len(e.Consentimientos) == 0 {
		t.Error("los consentimientos son lo que el PDF no expone; tienen que estar")
	}
	if len(e.Artefactos) == 0 {
		t.Fatal("sin artefactos no hay nada que cotejar")
	}
	for _, a := range e.Artefactos {
		if a.Hash == "" {
			t.Errorf("artefacto %q sin huella", a.Tipo)
		}
		if a.Bytes == 0 {
			t.Errorf("artefacto %q sin contenido", a.Tipo)
		}
		if !a.HashCuadra {
			t.Errorf("la huella de %q no cuadra: %s", a.Tipo, a.ProblemaHash)
		}
	}
	// Y todos los códigos, incluidos los que no modelamos.
	if len(e.Codigos) < 20 {
		t.Errorf("códigos = %d, se esperaban todos los del certificado", len(e.Codigos))
	}
}

// Una verificación que no se superó también se puede mirar: es justo cuando
// hace falta, y por eso no pasa por el filtro de DeCertificado.
func TestDeCertificadoCompleto_AunqueNoSeSupere(t *testing.T) {
	crudo := leerFixture(t)
	roto := strings.Replace(crudo,
		`blocker="true" code="TEST_LEGAL_AGE" condition="" message="" result="OK"`,
		`blocker="true" code="TEST_LEGAL_AGE" condition="" message="No es mayor de edad" result="KO"`, 1)
	if roto == crudo {
		t.Fatal("el fixture cambió")
	}

	e := DeCertificadoCompleto(parsea(t, roto))
	if e.Superada {
		t.Error("con una bloqueante fallida no se supera")
	}
	// Pero los atributos siguen ahí: sin ellos no se puede auditar qué pasó.
	if e.Atributos == nil || e.Atributos.NIF == "" {
		t.Error("los atributos tienen que verse aunque la verificación falle")
	}
	var vistaFallida bool
	for _, c := range e.Comprobaciones {
		if c.Codigo == "TEST_LEGAL_AGE" {
			vistaFallida = !c.Superada && c.Mensaje == "No es mayor de edad"
		}
	}
	if !vistaFallida {
		t.Error("la comprobación fallida tiene que verse con su motivo")
	}
}

func TestEvidencia_ServicioDesactivado(t *testing.T) {
	var s *Servicio
	if _, err := s.Evidencia(t.Context(), "guid"); err != ErrDesactivado {
		t.Errorf("se esperaba ErrDesactivado, se obtuvo %v", err)
	}
}

func leerFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/certificado-dni.xml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func parsea(t *testing.T, xml string) *wsdatachannel.IdentityCertificate {
	t.Helper()
	cert, err := wsdatachannel.ParseIdentityCertificate([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
