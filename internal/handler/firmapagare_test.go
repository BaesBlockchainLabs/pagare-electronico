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
	pedidas   []firma.Peticion
}

func (f *firmaFalsa) Activa() bool { return true }

// Devuelve la referencia que se le pide, como hace el servicio real: es lo que
// distingue un reintento del envío anterior.
func (f *firmaFalsa) Iniciar(_ context.Context, p firma.Peticion) (*firma.Envio, error) {
	f.pedidas = append(f.pedidas, p)
	return &firma.Envio{Referencia: p.Referencia, GUID: "GUID-1", HashOriginal: "aaaa"}, nil
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
	if err := regs.Crear(reg, []byte("%PDF a firmar")); err != nil {
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
	reg := registroPendiente(t, regs, firma.Endoso, pendienteTransferencia{
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

// La cesión espera a la firma igual que el endoso, y lo que se ejecuta después
// lleva su propia metadata: es un TRANSFER en el libro, pero de otro régimen.
func TestCompletar_FirmadoEjecutaLaCesion(t *testing.T) {
	ff := &firmaFalsa{
		situacion: &firma.Situacion{GUID: "GUID-1", Terminado: true, Firmado: true},
		firmado:   &firma.Firmado{PDF: []byte("%PDF"), Hash: "bbbb"},
	}
	h, regs, red := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Cesion, pendienteTransferencia{
		A: "pub-cesionario", PubFirmante: "pub-firm",
		Metadata: map[string]interface{}{
			"action": TipoOperacionCesion, "tipo_operacion": TipoOperacionCesion,
			"notificacion_fecha": "2026-09-12",
		},
	})

	if _, err := h.Completar(context.Background(), reg); err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if len(*red) != 1 {
		t.Fatalf("se esperaba una transferencia, hubo %d", len(*red))
	}
	cesion := (*red)[0]
	if cesion["to"] != "pub-cesionario" {
		t.Errorf("la cesión no fue al cesionario: %v", cesion)
	}
	meta, _ := cesion["metadata"].(map[string]interface{})
	if meta["tipo_operacion"] != TipoOperacionCesion {
		t.Errorf("la cesión no se marcó como tal: %v", meta)
	}
	if meta["notificacion_fecha"] != "2026-09-12" {
		t.Errorf("la constancia de la notificación se perdió: %v", meta)
	}
}

// La cesión va en el PDF aparte de los endosos: imprimirla entre ellos
// sugeriría una responsabilidad por la solvencia que el cedente no asumió.
func TestCesionParaPDF(t *testing.T) {
	fila := cesionParaPDF(&Cesion{
		Cesionario:        &models.Persona{Nombre: "Bea", Apellido: "Soler", NIF: "11111111H"},
		NotificacionFecha: "2026-09-12",
		NotificacionMedio: "burofax",
	}, "pub-cedente")

	if fila.Cesionario != "Bea Soler" || fila.NIF != "11111111H" {
		t.Errorf("cesionario = %q / %q", fila.Cesionario, fila.NIF)
	}
	if fila.CedentePub != "pub-cedente" {
		t.Errorf("cedente = %q", fila.CedentePub)
	}
	if fila.NotificacionFecha != "2026-09-12" || fila.NotificacionMedio != "burofax" {
		t.Errorf("la notificación no se conservó: %+v", fila)
	}
	if fila.Fecha == "" {
		t.Error("la cesión tiene que llevar fecha")
	}
}

// El asunto del aviso dice qué se firma: el firmante recibe un correo y tiene
// que saber si es una emisión, un endoso o una cesión.
func TestAsuntoDe(t *testing.T) {
	casos := map[firma.Operacion]string{
		firma.Emision: "emisión",
		firma.Endoso:  "endoso",
		firma.Cesion:  "cesión",
	}
	for op, esperado := range casos {
		if asunto := asuntoDe(op); !strings.Contains(asunto, esperado) {
			t.Errorf("asuntoDe(%q) = %q, se esperaba que mencionara %q", op, asunto, esperado)
		}
	}
}

func pideReintento(t *testing.T, h *PagareHandler, p *auth.Principal) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/pagares/firma",
		strings.NewReader(`{"id":"asset-1"}`))
	if p != nil {
		r = r.WithContext(auth.ContextWithPrincipal(r.Context(), p))
	}
	w := httptest.NewRecorder()
	h.PedirFirmaDeNuevo(w, r)
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	return w, res
}

var comoFirmante = &auth.Principal{UserID: "u1", Username: "rampa", Role: auth.RoleUser}

// Una firma fallida se puede volver a pedir, y se manda el mismo documento.
func TestPedirFirmaDeNuevo_TrasUnFallo(t *testing.T) {
	ff := &firmaFalsa{situacion: &firma.Situacion{
		GUID: "GUID-1", Terminado: true, Firmado: false, Descripcion: "Finalizada / Tiempo Expirado"}}
	h, regs, _ := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})
	if err := regs.Fallar(reg.ID, "Tiempo Expirado"); err != nil {
		t.Fatal(err)
	}

	w, res := pideReintento(t, h, comoFirmante)
	if w.Code != http.StatusOK {
		t.Fatalf("código = %d: %s", w.Code, w.Body.String())
	}
	if res["ok"] != true {
		t.Errorf("respuesta = %v", res)
	}

	nueva, err := regs.Ultima("asset-1")
	if err != nil {
		t.Fatal(err)
	}
	if nueva.ID == reg.ID {
		t.Error("tenía que abrirse otra firma, no reusar la fallida")
	}
	if !nueva.EnCurso() {
		t.Errorf("la nueva firma tiene que estar en curso: %q", nueva.Estado)
	}
	// El mismo documento: lo que se firma es lo que se pidió firmar.
	uno, _ := regs.PDFOriginal(reg)
	otro, _ := regs.PDFOriginal(nueva)
	if string(uno) != string(otro) {
		t.Error("el reintento mandó un documento distinto")
	}
	// Y la operación en espera viaja con ella.
	var p pendienteEmision
	if json.Unmarshal(nueva.Pendiente, &p) != nil || p.A != "pub-benef" {
		t.Errorf("la operación en espera se perdió: %s", nueva.Pendiente)
	}
}

// Con una firma en curso no se pide otra: sería un segundo aviso de lo mismo.
func TestPedirFirmaDeNuevo_EnCursoSeNiega(t *testing.T) {
	ff := &firmaFalsa{situacion: &firma.Situacion{GUID: "GUID-1", Terminado: false}}
	h, regs, _ := entornoFirma(t, ff)
	registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	w, _ := pideReintento(t, h, comoFirmante)
	if w.Code != http.StatusConflict {
		t.Fatalf("código = %d, se esperaba 409: %s", w.Code, w.Body.String())
	}
}

// Si resulta que ya estaba firmada, se dice y no se pide nada.
func TestPedirFirmaDeNuevo_YaFirmada(t *testing.T) {
	ff := &firmaFalsa{
		situacion: &firma.Situacion{GUID: "GUID-1", Terminado: true, Firmado: true},
		firmado:   &firma.Firmado{PDF: []byte("%PDF"), Hash: "bbbb"},
	}
	h, regs, _ := entornoFirma(t, ff)
	registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})

	w, res := pideReintento(t, h, comoFirmante)
	if w.Code != http.StatusOK || res["ok"] != true {
		t.Fatalf("código = %d: %s", w.Code, w.Body.String())
	}
	if f, _ := res["firma"].(map[string]interface{}); f["estado"] != string(firma.Firmada) {
		t.Errorf("tenía que decir que ya está firmada: %v", res["firma"])
	}
}

// Reintentar manda un aviso al móvil de una persona, así que no lo dispara
// cualquiera.
func TestPedirFirmaDeNuevo_SoloSuFirmante(t *testing.T) {
	ff := &firmaFalsa{}
	h, regs, _ := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})
	if err := regs.Fallar(reg.ID, "Tiempo Expirado"); err != nil {
		t.Fatal(err)
	}

	otro := &auth.Principal{UserID: "otro", Username: "otro", Role: auth.RoleUser}
	if w, _ := pideReintento(t, h, otro); w.Code != http.StatusForbidden {
		t.Errorf("código = %d, se esperaba 403", w.Code)
	}
	// Un administrador sí, que es quien desatasca.
	admin := &auth.Principal{UserID: "admin", Username: "admin", Role: auth.RoleAdmin}
	if w, _ := pideReintento(t, h, admin); w.Code != http.StatusOK {
		t.Errorf("un admin tenía que poder: %d", w.Code)
	}
}

// Sin sesión, nada.
func TestPedirFirmaDeNuevo_SinSesion(t *testing.T) {
	h, _, _ := entornoFirma(t, &firmaFalsa{})
	if w, _ := pideReintento(t, h, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("código = %d, se esperaba 401", w.Code)
	}
}

// Un reintento tiene que llevar otra referencia: la anterior ya identifica un
// envío en el portal, y consultar por ella devolvería el viejo.
func TestPedirFirmaDeNuevo_OtraReferencia(t *testing.T) {
	ff := &firmaFalsa{}
	h, regs, _ := entornoFirma(t, ff)
	reg := registroPendiente(t, regs, firma.Emision, pendienteEmision{A: "pub-benef", PubFirmante: "pub-firm"})
	if err := regs.Fallar(reg.ID, "Tiempo Expirado"); err != nil {
		t.Fatal(err)
	}

	if w, _ := pideReintento(t, h, comoFirmante); w.Code != http.StatusOK {
		t.Fatalf("código = %d", w.Code)
	}
	if len(ff.pedidas) != 1 {
		t.Fatalf("peticiones al portal = %d", len(ff.pedidas))
	}
	if ff.pedidas[0].Referencia == reg.Referencia {
		t.Errorf("el reintento reusó la referencia anterior: %q", ff.pedidas[0].Referencia)
	}
	if ff.pedidas[0].Asunto == "" {
		t.Error("el aviso tiene que llevar asunto")
	}
	if len(ff.pedidas[0].PDF) == 0 {
		t.Error("el reintento tiene que mandar el documento")
	}
}

// Un listado necesita el estado de todas sus firmas de una vez, y esa consulta
// no puede hablar con el portal: se pinta a menudo.
func TestEstadoFirmas_EnUnaSolaConsulta(t *testing.T) {
	ff := &firmaFalsa{situacion: &firma.Situacion{Terminado: true, Firmado: true}}
	h, regs, _ := entornoFirma(t, ff)

	uno := &firma.Registro{AssetID: "asset-1", Operacion: firma.Emision, UserID: "u1",
		Referencia: "r1", HashOriginal: "aaaa"}
	if err := regs.Crear(uno, []byte("%PDF")); err != nil {
		t.Fatal(err)
	}
	dos := &firma.Registro{AssetID: "asset-2", Operacion: firma.Endoso, UserID: "u1",
		Referencia: "r2", HashOriginal: "bbbb"}
	if err := regs.Crear(dos, []byte("%PDF")); err != nil {
		t.Fatal(err)
	}
	if err := regs.Fallar(dos.ID, "Tiempo Expirado"); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/pagares/firmas?ids=asset-1,asset-2,asset-sin-firma", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), comoFirmante))
	w := httptest.NewRecorder()
	h.EstadoFirmas(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("código = %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		OK     bool                      `json:"ok"`
		Activa bool                      `json:"activa"`
		Firmas map[string]map[string]any `json:"firmas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Activa {
		t.Fatalf("respuesta = %s", w.Body.String())
	}
	if len(res.Firmas) != 2 {
		t.Errorf("firmas = %d, se esperaban 2 (el tercero no tiene)", len(res.Firmas))
	}
	if res.Firmas["asset-1"]["estado"] != string(firma.Pendiente) {
		t.Errorf("asset-1 = %v", res.Firmas["asset-1"])
	}
	if res.Firmas["asset-2"]["estado"] != string(firma.Fallida) {
		t.Errorf("asset-2 = %v", res.Firmas["asset-2"])
	}
	// Sólo lectura: no puede haber consultado el portal ni recogido nada.
	if len(ff.recogidas) != 0 {
		t.Errorf("un listado no puede hablar con el portal: %v", ff.recogidas)
	}
}

// Sin firma configurada, el listado no se entera de nada y no revienta.
func TestEstadoFirmas_Desactivada(t *testing.T) {
	h := NewPagareHandler(nil, nil, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/pagares/firmas?ids=a", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), comoFirmante))
	w := httptest.NewRecorder()
	h.EstadoFirmas(w, r)

	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != http.StatusOK || res["activa"] != false {
		t.Errorf("código = %d, respuesta = %v", w.Code, res)
	}
}
