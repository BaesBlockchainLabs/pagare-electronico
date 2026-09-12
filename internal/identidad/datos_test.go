package identidad

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/BaesBlockchainLabs/logalsend-go/wsdatachannel"
)

func certificado(t *testing.T, ruta string) *wsdatachannel.IdentityCertificate {
	t.Helper()
	crudo, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatalf("leyendo %s: %v", ruta, err)
	}
	cert, err := wsdatachannel.ParseIdentityCertificate(crudo)
	if err != nil {
		t.Fatalf("parseando %s: %v", ruta, err)
	}
	return cert
}

// El certificado de ejemplo lleva datos anonimizados: el DNI 00000000T de
// NOMBRE APELLIDOUNO APELLIDODOS, nacida el 4 de marzo de 1980.
func TestDeCertificado_ExtraeLosCamposDelDocumento(t *testing.T) {
	d, err := DeCertificado(certificado(t, "testdata/certificado-dni.xml"))
	if err != nil {
		t.Fatalf("DeCertificado: %v", err)
	}

	casos := []struct {
		campo    string
		obtenido string
		esperado string
	}{
		{"NIF", d.NIF, "00000000T"},
		{"documento", d.Documento, "DNI"},
		{"número de soporte", d.NumeroSoporte, "ABC000000"},
		{"nombre", d.Nombre, "NOMBRE"},
		{"apellido 1", d.Apellido1, "APELLIDOUNO"},
		{"apellido 2", d.Apellido2, "APELLIDODOS"},
		{"apellidos", d.Apellidos(), "APELLIDOUNO APELLIDODOS"},
		{"nombre completo", d.NombreCompleto(), "NOMBRE APELLIDOUNO APELLIDODOS"},
		{"sexo", d.Sexo, "Mujer"},
		{"fecha de nacimiento", d.FechaNacimiento, "1980-03-04"},
		{"lugar de nacimiento", d.LugarNacimiento, "CIUDAD"},
		{"domicilio completo", d.DomicilioCompleto, "CALLE FALSA 1-CIUDAD-PROVINCIA"},
		{"dirección", d.Direccion, "CALLE FALSA 1"},
		{"localidad", d.Localidad, "CIUDAD"},
		{"provincia", d.Provincia, "PROVINCIA"},
		{"expedición", d.Expedicion, "2020-01-02"},
		{"caducidad", d.Caducidad, "2030-01-02"},
		{"método", d.Metodo, "NFC"},
	}
	for _, c := range casos {
		if c.obtenido != c.esperado {
			t.Errorf("%s = %q, se esperaba %q", c.campo, c.obtenido, c.esperado)
		}
	}

	// La trazabilidad de la evidencia tiene que salir del certificado, o no hay
	// forma de volver a pedirlo después.
	if d.TokenID == "" {
		t.Error("el token de Logalty no se extrajo")
	}
	if d.Fecha.IsZero() {
		t.Error("la fecha del resultado no se extrajo")
	}
}

// Una comprobación bloqueante fallida invalida la verificación aunque el envío
// haya terminado bien, y el motivo tiene que llegar hasta el usuario.
func TestDeCertificado_RechazaComprobacionBloqueanteFallida(t *testing.T) {
	crudo, err := os.ReadFile("testdata/certificado-dni.xml")
	if err != nil {
		t.Fatal(err)
	}
	roto := strings.Replace(string(crudo),
		`blocker="true" code="TEST_LEGAL_AGE" condition="" message="" result="OK"`,
		`blocker="true" code="TEST_LEGAL_AGE" condition="" message="No es mayor de edad" result="KO"`, 1)
	if roto == string(crudo) {
		t.Fatal("el fixture cambió: no se pudo forzar el fallo de la comprobación")
	}

	cert, err := wsdatachannel.ParseIdentityCertificate([]byte(roto))
	if err != nil {
		t.Fatal(err)
	}

	_, err = DeCertificado(cert)
	var noSuperada *ErrNoSuperada
	if !errors.As(err, &noSuperada) {
		t.Fatalf("se esperaba *ErrNoSuperada, se obtuvo %v", err)
	}
	if !strings.Contains(noSuperada.Error(), "No es mayor de edad") {
		t.Errorf("el motivo no llega al usuario: %q", noSuperada.Error())
	}
}

// Una comprobación no bloqueante fallida no invalida nada: el fixture trae
// TEST_NFC_CHIP_PRESENCE en KO con blocker=false y aun así verifica.
func TestDeCertificado_IgnoraComprobacionNoBloqueante(t *testing.T) {
	if _, err := DeCertificado(certificado(t, "testdata/certificado-dni.xml")); err != nil {
		t.Fatalf("una comprobación no bloqueante no debería invalidar: %v", err)
	}
}

func TestDeCertificado_SinCertificado(t *testing.T) {
	if _, err := DeCertificado(nil); err == nil {
		t.Fatal("un certificado vacío tiene que fallar")
	}
}

// El servicio desactivado se comporta como tal en lugar de reventar: es el
// modo en el que corre el desarrollo.
func TestServicioDesactivado(t *testing.T) {
	var s *Servicio
	if s.Activo() {
		t.Fatal("un servicio nil no está activo")
	}
	if _, err := s.Iniciar(t.Context(), Solicitud{Movil: "+34600000000"}); !errors.Is(err, ErrDesactivado) {
		t.Errorf("Iniciar: se esperaba ErrDesactivado, se obtuvo %v", err)
	}
	if _, err := s.Consultar(t.Context(), "ref"); !errors.Is(err, ErrDesactivado) {
		t.Errorf("Consultar: se esperaba ErrDesactivado, se obtuvo %v", err)
	}
	if _, err := s.Recoger(t.Context(), "guid"); !errors.Is(err, ErrDesactivado) {
		t.Errorf("Recoger: se esperaba ErrDesactivado, se obtuvo %v", err)
	}
}

// El certificado da el domicilio en un solo campo y no trae código postal: el
// chip del DNI no lo lleva.
func TestPartirDomicilio(t *testing.T) {
	casos := []struct {
		nombre                    string
		domicilio                 string
		via, localidad, provincia string
	}{
		{"vía, municipio y provincia, con la provincia en dos lenguas",
			"POL. NO EL BULL 25-MONOVAR-ALICANTE/ALACANT",
			"POL. NO EL BULL 25", "MONOVAR", "ALICANTE/ALACANT"},
		{"un guion dentro del nombre de la vía no despista",
			"AVDA. RUIZ-PICASSO 3-MADRID-MADRID",
			"AVDA. RUIZ-PICASSO 3", "MADRID", "MADRID"},
		{"sin provincia",
			"CALLE MAYOR 10-TERUEL", "CALLE MAYOR 10", "TERUEL", ""},
		{"un solo tramo se deja entero antes que repartirlo mal",
			"CALLE MAYOR 10", "CALLE MAYOR 10", "", ""},
		{"los espacios sobrantes no cuentan",
			" CALLE MAYOR 10 - TERUEL - TERUEL ", "CALLE MAYOR 10", "TERUEL", "TERUEL"},
		{"vacío", "", "", "", ""},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			via, localidad, provincia := partirDomicilio(c.domicilio)
			if via != c.via || localidad != c.localidad || provincia != c.provincia {
				t.Errorf("partirDomicilio(%q) = (%q, %q, %q), se esperaba (%q, %q, %q)",
					c.domicilio, via, localidad, provincia, c.via, c.localidad, c.provincia)
			}
		})
	}
}

// El país sale del código de país del SOD del chip, que es quien emitió el
// documento del que también sale el domicilio.
func TestDeCertificado_PaisDelChip(t *testing.T) {
	d, err := DeCertificado(certificado(t, "testdata/certificado-dni.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Pais != "ES" {
		t.Errorf("país = %q, se esperaba \"ES\"", d.Pais)
	}
	if d.Nacionalidad == "" {
		t.Error("la nacionalidad del documento no se extrajo")
	}
}
