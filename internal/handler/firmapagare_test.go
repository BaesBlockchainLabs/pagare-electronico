package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"pagare/internal/auth"
	"pagare/internal/bcfclient"
	"pagare/internal/config"
	"pagare/internal/firma"
	"pagare/internal/models"
)

// firmaFalsa hace de portal: se le dice qué contestar y apunta lo que le piden.
type firmaFalsa struct {
	situacion *firma.Situacion
	errorSit  error
	firmado   *firma.Firmado
	errorRec  error
	recogidas []string
}

func (f *firmaFalsa) Activa() bool { return true }

func (f *firmaFalsa) Iniciar(context.Context, firma.Peticion) (*firma.Envio, error) {
	return &firma.Envio{Referencia: "ref", GUID: "GUID-1", HashOriginal: "aaaa"}, nil
}

func (f *firmaFalsa) Consultar(context.Context, string) (*firma.Situacion, error) {
	return f.situacion, f.errorSit
}

func (f *firmaFalsa) Recoger(_ context.Context, guid, _ string) (*firma.Firmado, error) {
	f.recogidas = append(f.recogidas, guid)
	return f.firmado, f.errorRec
}

// firmantesFalsos devuelve siempre el mismo firmante.
type firmantesFalsos struct{}

func (firmantesFalsos) GetByID(string) (*auth.User, error) {
	return &auth.User{ID: "u1", Nombre: "RAMON", Apellido: "MARTINEZ",
		NIF: "22133609L", Email: "r@example.com", Telefono: "+34600000000"}, nil
}

// clavesFalsas abre la privada sin keyvault.
type clavesFalsas struct{ err error }

func (c clavesFalsas) GetPrivateKey(string, string) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	return "pvt-de-prueba", nil
}
func (c clavesFalsas) EnsureUserKeypair(string) (string, error) { return "pub-de-prueba", nil }

// entornoFirma monta un handler con portal falso y una red que apunta lo que
// se le manda.
func entornoFirma(t *testing.T, ff *firmaFalsa) (*PagareHandler, *firma.Registros, *[]map[string]interface{}) {
	t.Helper()
	var recibido []map[string]interface{}
	red := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cuerpo map[string]interface{}
		json.NewDecoder(r.Body).Decode(&cuerpo)
		recibido = append(recibido, cuerpo)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"asset-1","cost":0}`)
	}))
	t.Cleanup(red.Close)

	regs, err := firma.AbrirRegistros(t.TempDir())
	if err != nil {
		t.Fatalf("AbrirRegistros: %v", err)
	}
	t.Cleanup(func() { regs.Cerrar() })

	h := NewPagareHandler(bcfclient.New(config.BlockchainConfig{BaseURL: red.URL}), nil, clavesFalsas{})
	h.SetFirma(ff, regs, firmantesFalsos{})
	return h, regs, &recibido
}

func registroPendiente(t *testing.T, regs *firma.Registros, op firma.Operacion, espera any) *firma.Registro {
	t.Helper()
	bruto, _ := json.Marshal(espera)
	reg := &firma.Registro{AssetID: "asset-1", Operacion: op, UserID: "u1",
		Referencia: "ref", HashOriginal: "aaaa", GUID: "GUID-1", Pendiente: bruto}
	if err := regs.Crear(reg); err != nil {
		t.Fatalf("Crear: %v", err)
	}
	return reg
}

// Mientras el envío no ha terminado, no se recoge nada ni se toca la cadena.
func TestCompletar_SinTerminarNoHaceNada(t *testing.T) {
	ff := &firmaFalsa{situacion: &firma.Situacion{GUID: "GUID-1", Terminado: false}}
	h, regs, red := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	reg, err := h.Completar(context.Background(), reg)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if !reg.EnCurso() {
		t.Error("la firma tiene que seguir en curso")
	}
	if len(ff.recogidas) != 0 {
		t.Error("no debería descargar nada todavía")
	}
	if len(*red) != 0 {
		t.Errorf("no debería tocar la cadena: %v", *red)
	}
}

// Terminado sin firmar cierra la firma con el motivo del portal, y la entrega
// no se hace.
func TestCompletar_NoFirmadoCierraYNoEntrega(t *testing.T) {
	ff := &firmaFalsa{situacion: &firma.Situacion{
		GUID: "GUID-1", Terminado: true, Firmado: false,
		Descripcion: "Finalizada / Tiempo Expirado"}}
	h, regs, red := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	reg, err := h.Completar(context.Background(), reg)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if reg.Estado != firma.Fallida || reg.Motivo != "Finalizada / Tiempo Expirado" {
		t.Errorf("no se cerró con su motivo: %+v", reg)
	}
	if len(*red) != 0 {
		t.Errorf("sin firma no puede haber entrega: %v", *red)
	}
	guardado, _ := regs.Ultima("asset-1")
	if guardado.Estado != firma.Fallida {
		t.Errorf("el estado no se guardó: %q", guardado.Estado)
	}
}

// Firmado: se guarda el PDF y sólo entonces se entrega al beneficiario.
func TestCompletar_FirmadoEntregaAlBeneficiario(t *testing.T) {
	ff := &firmaFalsa{
		situacion: &firma.Situacion{GUID: "GUID-1", Terminado: true, Firmado: true},
		firmado:   &firma.Firmado{PDF: []byte("%PDF firmado"), Hash: "bbbb", HashOriginal: "aaaa"},
	}
	h, regs, red := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	reg, err := h.Completar(context.Background(), reg)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if reg.Estado != firma.Firmada || reg.HashFirmado != "bbbb" {
		t.Fatalf("no se cerró como firmada: %+v", reg)
	}
	if len(*red) != 1 {
		t.Fatalf("se esperaba una llamada a la cadena, hubo %d", len(*red))
	}
	entrega := (*red)[0]
	if entrega["to"] != "pub-benef" || entrega["id"] != "asset-1" {
		t.Errorf("la entrega no fue al beneficiario: %v", entrega)
	}
	meta, _ := entrega["metadata"].(map[string]interface{})
	if meta["tipo_operacion"] != TipoOperacionEntrega {
		t.Errorf("la entrega no se marcó como tal: %v", meta)
	}
	if pdf, err := regs.PDFFirmado(reg); err != nil || string(pdf) != "%PDF firmado" {
		t.Errorf("el PDF firmado no se guardó: %v", err)
	}
}

// Un beneficiario sin identidad no impide la firma: el pagaré queda firmado y
// pendiente de entrega, que es un estado legítimo.
func TestCompletar_SinBeneficiarioNoEntregaPeroFirma(t *testing.T) {
	ff := &firmaFalsa{
		situacion: &firma.Situacion{GUID: "GUID-1", Terminado: true, Firmado: true},
		firmado:   &firma.Firmado{PDF: []byte("%PDF"), Hash: "bbbb"},
	}
	h, regs, red := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "", PubFirmante: "pub-firm"})

	reg, err := h.Completar(context.Background(), reg)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if reg.Estado != firma.Firmada {
		t.Errorf("estado = %q, se esperaba firmada", reg.Estado)
	}
	if len(*red) != 0 {
		t.Errorf("no había a quién entregar: %v", *red)
	}
}

// En un endoso, lo que espera a la firma es el cambio de titularidad.
func TestCompletar_FirmadoEjecutaElEndoso(t *testing.T) {
	ff := &firmaFalsa{
		situacion: &firma.Situacion{GUID: "GUID-1", Terminado: true, Firmado: true},
		firmado:   &firma.Firmado{PDF: []byte("%PDF"), Hash: "bbbb"},
	}
	h, regs, red := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Endoso, pendienteEndoso{
		A: "pub-endosatario", PubFirmante: "pub-firm",
		Metadata: map[string]interface{}{"action": "TRANSFER", "tipo_endoso": "en_propiedad"},
	})

	if _, err := h.Completar(context.Background(), reg); err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if len(*red) != 1 {
		t.Fatalf("se esperaba una llamada a la cadena, hubo %d", len(*red))
	}
	endoso := (*red)[0]
	if endoso["to"] != "pub-endosatario" {
		t.Errorf("el endoso no fue al endosatario: %v", endoso)
	}
	meta, _ := endoso["metadata"].(map[string]interface{})
	if meta["tipo_endoso"] != "en_propiedad" {
		t.Errorf("la metadata del endoso no se conservó: %v", meta)
	}
}

// Que el portal haya firmado otro documento cierra la firma: guardarla sería
// peor que no tener ninguna. Y no se ejecuta nada.
func TestCompletar_OtroDocumentoCierraLaFirma(t *testing.T) {
	ff := &firmaFalsa{
		situacion: &firma.Situacion{GUID: "GUID-1", Terminado: true, Firmado: true},
		errorRec:  errors.New("firma: el portal firmó otro documento: se mandó aaaa y devuelve cccc"),
	}
	h, regs, red := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	reg, err := h.Completar(context.Background(), reg)
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if reg.Estado != firma.Fallida {
		t.Errorf("estado = %q, se esperaba fallida", reg.Estado)
	}
	if len(*red) != 0 {
		t.Errorf("no puede entregarse con una firma que no cuadra: %v", *red)
	}
}

// Un fallo de red al descargar deja la firma pendiente: reintentar sí puede
// cambiarlo.
func TestCompletar_FalloDeRedNoCierraLaFirma(t *testing.T) {
	ff := &firmaFalsa{
		situacion: &firma.Situacion{GUID: "GUID-1", Terminado: true, Firmado: true},
		errorRec:  errors.New("firma: descargando dwn_signed: dial tcp: connection refused"),
	}
	h, regs, _ := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	reg, err := h.Completar(context.Background(), reg)
	if err == nil {
		t.Fatal("un fallo de red tiene que reportarse")
	}
	if !reg.EnCurso() {
		t.Errorf("estado = %q, la firma tiene que seguir pendiente", reg.Estado)
	}
}

// Sin firma configurada el handler no estorba: es el modo de desarrollo.
func TestFirmaActiva_SinConfigurar(t *testing.T) {
	h := NewPagareHandler(nil, nil, nil)
	if h.FirmaActiva() {
		t.Error("sin SetFirma la firma no puede estar activa")
	}
	// Un servicio nil también significa desactivada, y no puede reventar.
	var svc *firma.Servicio
	h.SetFirma(svc, nil, nil)
	if h.FirmaActiva() {
		t.Error("con servicio nil la firma no puede estar activa")
	}
}

// La referencia distingue operación y reintentos, porque es con lo que se
// consulta el envío en el portal.
func TestReferenciaDe(t *testing.T) {
	casos := []struct {
		op       firma.Operacion
		intento  int
		esperada string
	}{
		{firma.Emision, 1, "pagare-asset-1-emision"},
		{firma.Emision, 0, "pagare-asset-1-emision"},
		{firma.Endoso, 1, "pagare-asset-1-endoso"},
		{firma.Endoso, 3, "pagare-asset-1-endoso-3"},
	}
	for _, c := range casos {
		if got := referenciaDe(c.op, "asset-1", c.intento); got != c.esperada {
			t.Errorf("referenciaDe(%q, %d) = %q, se esperaba %q", c.op, c.intento, got, c.esperada)
		}
	}
}

// El endoso que se firma va en el PDF aunque no esté todavía en la cadena.
func TestEndosoParaPDF(t *testing.T) {
	fila := endosoParaPDF(&models.Endoso{
		Tipo:        "en_propiedad",
		Fecha:       "2026-09-12T18:30:00Z",
		Clausula:    "sin gastos",
		Endosatario: &models.Persona{Nombre: "Bea", Apellido: "Soler", NIF: "11111111H"},
	}, "pub-endosante")

	if fila.Fecha != "2026-09-12" {
		t.Errorf("fecha = %q, se esperaba sólo el día", fila.Fecha)
	}
	if fila.Endosatario != "Bea Soler" || fila.NIF != "11111111H" {
		t.Errorf("endosatario = %q / %q", fila.Endosatario, fila.NIF)
	}
	if fila.EndosantePub != "pub-endosante" || fila.Tipo != "en_propiedad" {
		t.Errorf("fila incompleta: %+v", fila)
	}
}
