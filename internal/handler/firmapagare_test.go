package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
		// Sólo se apuntan las escrituras: un GET no mueve el título, y contarlo
		// convertiría cualquier lectura en un falso positivo.
		if r.Method != http.MethodGet {
			var cuerpo map[string]interface{}
			json.NewDecoder(r.Body).Decode(&cuerpo)
			recibido = append(recibido, cuerpo)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"id":"asset-1","cost":0,"owners":[]}`)
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

// La pantalla de entrega pendiente llama a Entregar, que no sabía nada de
// firmas: con esto el título no se mueve mientras la firma esté sin hacer, que
// es lo que daba sentido a dejar la entrega en espera.
func TestEntregar_NoSeSaltaLaFirmaPendiente(t *testing.T) {
	ff := &firmaFalsa{situacion: &firma.Situacion{Terminado: false}}
	h, regs, red := entornoFirma(t, ff)
	registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	cuerpo := `{"id":"asset-1","to":"pub-benef"}`
	r := httptest.NewRequest(http.MethodPut, "/api/pagares/entrega", strings.NewReader(cuerpo))
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(),
		&auth.Principal{UserID: "u1", Username: "rampa", Role: auth.RoleUser}))
	w := httptest.NewRecorder()

	h.Entregar(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("código = %d, se esperaba 409: %s", w.Code, w.Body.String())
	}
	if len(*red) != 0 {
		t.Errorf("no puede tocar la cadena con la firma pendiente: %v", *red)
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["firma"] == nil {
		t.Error("la respuesta tiene que decir en qué estado está la firma")
	}
}

// Firmada pero con la entrega sin ejecutar no bloquea: entregar es justo lo
// que faltaba.
func TestFirmaSinCompletar_FirmadaNoBloquea(t *testing.T) {
	ff := &firmaFalsa{}
	h, regs, _ := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	if h.FirmaSinCompletar("asset-1") == nil {
		t.Error("una firma pendiente tiene que bloquear")
	}
	if err := regs.Resolver(reg, &firma.Firmado{PDF: []byte("%PDF"), Hash: "bbbb"}); err != nil {
		t.Fatal(err)
	}
	if h.FirmaSinCompletar("asset-1") != nil {
		t.Error("una firma ya hecha no puede bloquear la entrega que esperaba")
	}
}

// Sin firma configurada, Entregar no cambia de comportamiento.
func TestFirmaSinCompletar_DesactivadaNoBloquea(t *testing.T) {
	h := NewPagareHandler(nil, nil, nil)
	if h.FirmaSinCompletar("asset-1") != nil {
		t.Error("sin firma configurada no puede bloquear nada")
	}
}

func pideEntrega(t *testing.T, h *PagareHandler) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/api/pagares/entrega",
		strings.NewReader(`{"id":"asset-1","to":"pub-benef"}`))
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(),
		&auth.Principal{UserID: "u1", Username: "rampa", Role: auth.RoleUser}))
	w := httptest.NewRecorder()
	h.Entregar(w, r)

	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	return w, res
}

// Quien pulsa entregar suele venir de firmar, así que el botón mira primero si
// la firma ya está y, si lo está, entrega en el acto.
func TestEntregar_RecogeLaFirmaYEntrega(t *testing.T) {
	ff := &firmaFalsa{
		situacion: &firma.Situacion{GUID: "GUID-1", Terminado: true, Firmado: true},
		firmado:   &firma.Firmado{PDF: []byte("%PDF firmado"), Hash: "bbbb"},
	}
	h, regs, red := entornoFirma(t, ff)
	registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	w, res := pideEntrega(t, h)
	if w.Code != http.StatusOK {
		t.Fatalf("código = %d, se esperaba 200: %s", w.Code, w.Body.String())
	}
	if res["ok"] != true {
		t.Errorf("respuesta = %v", res)
	}
	if len(*red) != 1 {
		t.Fatalf("se esperaba una transferencia, hubo %d", len(*red))
	}
	if (*red)[0]["to"] != "pub-benef" {
		t.Errorf("la entrega no fue al beneficiario: %v", (*red)[0])
	}

	guardado, _ := regs.Ultima("asset-1")
	if guardado.Estado != firma.Firmada || guardado.EjecutadaAt == nil {
		t.Errorf("la firma no quedó completa: %+v", guardado)
	}
}

// Y si la firma falló, entregar se niega: no hay firma que respalde el título.
func TestEntregar_FirmaFallidaSeNiega(t *testing.T) {
	ff := &firmaFalsa{situacion: &firma.Situacion{
		GUID: "GUID-1", Terminado: true, Firmado: false,
		Descripcion: "Finalizada / Tiempo Expirado"}}
	h, regs, red := entornoFirma(t, ff)
	registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	w, res := pideEntrega(t, h)
	if w.Code != http.StatusConflict {
		t.Fatalf("código = %d, se esperaba 409: %s", w.Code, w.Body.String())
	}
	if len(*red) != 0 {
		t.Errorf("sin firma no puede haber entrega: %v", *red)
	}
	if f, _ := res["firma"].(map[string]interface{}); f["estado"] != string(firma.Fallida) {
		t.Errorf("la respuesta tiene que decir que la firma falló: %v", res["firma"])
	}
}

// Una firma firmada cuya entrega falló antes no bloquea: se sigue por el
// camino normal y se reintenta.
func TestEntregar_FirmadaAMediasSigueAdelante(t *testing.T) {
	ff := &firmaFalsa{}
	h, regs, _ := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})
	if err := regs.Resolver(reg, &firma.Firmado{PDF: []byte("%PDF"), Hash: "bbbb"}); err != nil {
		t.Fatal(err)
	}

	w, _ := pideEntrega(t, h)
	// Llega al camino normal: lo que conteste la red ya no es cosa del guardia,
	// pero no puede ser un 409 de firma pendiente.
	if w.Code == http.StatusConflict {
		t.Errorf("una firma ya hecha no puede bloquear la entrega: %s", w.Body.String())
	}
}
