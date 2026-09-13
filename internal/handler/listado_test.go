package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pagare/internal/auth"
	"pagare/internal/bcfclient"
	"pagare/internal/config"
)

// libroFalso imita al libro: apunta las consultas que recibe y tarda un poco en
// cada histórico, que es lo que hace lento al listado de verdad.
type libroFalso struct {
	assets       int
	porPagina    int
	historicos   atomic.Int32
	propietarios atomic.Int32
	consultas    []map[string]interface{}
	latencia     time.Duration
}

func (l *libroFalso) servidor(t *testing.T) *bcfclient.Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasPrefix(r.URL.Path, "/asset/owners") {
			l.propietarios.Add(1)
			fmt.Fprint(w, `{"ok":true,"owners":[]}`)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/asset/history") {
			l.historicos.Add(1)
			time.Sleep(l.latencia)
			fmt.Fprint(w, `{"ok":true,"history":[]}`)
			return
		}

		var q map[string]interface{}
		json.Unmarshal([]byte(r.URL.Query().Get("query")), &q)
		l.consultas = append(l.consultas, q)

		porPagina := l.porPagina
		if n, ok := q["per_page"].(float64); ok {
			porPagina = int(n)
		}
		if porPagina <= 0 || porPagina > l.assets {
			porPagina = l.assets
		}
		var b strings.Builder
		b.WriteString(`{"ok":true,"count":{"total":`)
		fmt.Fprintf(&b, `%d,"pages":{"current":1,"per_page":%d,"total":%d}},"assets":[`,
			l.assets, porPagina, (l.assets+porPagina-1)/porPagina)
		for i := 0; i < porPagina; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"id":"asset-%d","data":{"type":"pagare_electronico"}}`, i)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	}))
	t.Cleanup(s.Close)
	return bcfclient.New(config.BlockchainConfig{BaseURL: s.URL})
}

func pideListado(t *testing.T, h *ConsultaHandler, consulta string) map[string]interface{} {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/pagares?"+consulta, nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(),
		&auth.Principal{UserID: "u1", Username: "ana", Role: auth.RoleAdmin}))
	w := httptest.NewRecorder()
	h.ListPagares(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("código = %d: %s", w.Code, w.Body.String())
	}
	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	return res
}

// El listado pide una página, no el libro entero, y del más nuevo al más
// antiguo. Sin esto, resolver el estado costaba una consulta al histórico por
// pagaré y crecía sin tope con el uso.
func TestListPagares_PaginaYOrdenInverso(t *testing.T) {
	libro := &libroFalso{assets: 200, porPagina: 25}
	h := NewConsultaHandler(libro.servidor(t))

	pideListado(t, h, "")

	if len(libro.consultas) != 1 {
		t.Fatalf("consultas = %d", len(libro.consultas))
	}
	q := libro.consultas[0]
	if q["per_page"] != float64(paginacionPorDefecto) {
		t.Errorf("per_page = %v, se esperaba %d", q["per_page"], paginacionPorDefecto)
	}
	if q["page_num"] != float64(1) {
		t.Errorf("page_num = %v", q["page_num"])
	}
	if q["inverse"] != true {
		t.Errorf("inverse = %v: la cartera interesa del más nuevo al más antiguo", q["inverse"])
	}
	// Y sólo se resuelve el estado de la página, no el de los 200.
	if n := libro.historicos.Load(); n != int32(paginacionPorDefecto) {
		t.Errorf("consultas al histórico = %d, se esperaban %d", n, paginacionPorDefecto)
	}
}

func TestListPagares_PaginaPedida(t *testing.T) {
	libro := &libroFalso{assets: 200, porPagina: 10}
	h := NewConsultaHandler(libro.servidor(t))

	pideListado(t, h, "page=3&per_page=10")
	q := libro.consultas[0]
	if q["page_num"] != float64(3) || q["per_page"] != float64(10) {
		t.Errorf("paginación = %v / %v", q["page_num"], q["per_page"])
	}
}

// El cliente no puede pedir el libro entero disfrazado de página.
func TestListPagares_TopePorPagina(t *testing.T) {
	libro := &libroFalso{assets: 500, porPagina: 25}
	h := NewConsultaHandler(libro.servidor(t))

	pideListado(t, h, "per_page=5000")
	if q := libro.consultas[0]; q["per_page"] != float64(100) {
		t.Errorf("per_page = %v, se esperaba el tope de 100", q["per_page"])
	}
}

// Los estados se resuelven en paralelo: en serie, una página de 25 sumaba 25
// latencias una detrás de otra.
func TestListPagares_ResuelveEnParalelo(t *testing.T) {
	libro := &libroFalso{assets: 24, porPagina: 24, latencia: 20 * time.Millisecond}
	h := NewConsultaHandler(libro.servidor(t))

	inicio := time.Now()
	pideListado(t, h, "per_page=24")
	tardanza := time.Since(inicio)

	enSerie := 24 * 20 * time.Millisecond
	if tardanza > enSerie/2 {
		t.Errorf("tardó %v; en serie serían %v, así que no está paralelizando", tardanza, enSerie)
	}
	if n := libro.historicos.Load(); n != 24 {
		t.Errorf("consultas al histórico = %d, se esperaban 24", n)
	}
}

// Con caché, repintar el listado no vuelve a preguntar por los estados.
func TestListPagares_LaCacheAhorraConsultas(t *testing.T) {
	libro := &libroFalso{assets: 10, porPagina: 10}
	h := NewConsultaHandler(libro.servidor(t))
	h.SetCacheEstados(NuevaCacheEstados())

	pideListado(t, h, "per_page=10")
	primera := libro.historicos.Load()
	if primera != 10 {
		t.Fatalf("primera pasada = %d consultas", primera)
	}

	pideListado(t, h, "per_page=10")
	if n := libro.historicos.Load(); n != primera {
		t.Errorf("la segunda pasada preguntó %d veces más", n-primera)
	}

	// Y olvidar uno hace que sólo se vuelva a preguntar por ése.
	h.estados.Olvidar("asset-3")
	pideListado(t, h, "per_page=10")
	if n := libro.historicos.Load(); n != primera+1 {
		t.Errorf("tras olvidar uno se preguntó %d veces, se esperaba 1", n-primera)
	}
}

// A un administrador no se le resuelve la titularidad: los ve todos igual, así
// que preguntarla sería una consulta por pagaré tirada.
func TestListPagares_AdminNoPreguntaPropietarios(t *testing.T) {
	libro := &libroFalso{assets: 10, porPagina: 10}
	h := NewConsultaHandler(libro.servidor(t))

	pideListado(t, h, "per_page=10")
	if n := libro.propietarios.Load(); n != 0 {
		t.Errorf("preguntó %d veces por propietarios siendo admin", n)
	}
}

// A un usuario normal sí, y sólo mira el histórico de los que no son suyos por
// propiedad directa.
func TestListPagares_UsuarioFiltraPorTitularidad(t *testing.T) {
	libro := &libroFalso{assets: 4, porPagina: 4}
	h := NewConsultaHandler(libro.servidor(t))

	r := httptest.NewRequest(http.MethodGet, "/api/pagares?per_page=4", nil)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(),
		&auth.Principal{UserID: "u1", Username: "ana", Role: auth.RoleUser}))
	w := httptest.NewRecorder()
	h.ListPagares(w, r)

	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	assets, _ := res["assets"].([]interface{})
	// El libro falso no devuelve propietarios ni histórico con su clave, así
	// que no es suyo ninguno.
	if len(assets) != 0 {
		t.Errorf("assets = %d, no es suyo ninguno", len(assets))
	}
	if n := libro.propietarios.Load(); n != 4 {
		t.Errorf("consultas de propietarios = %d, se esperaban 4", n)
	}
	// Y el recuento global no se le enseña, pero sí cuántas páginas hay.
	count, _ := res["count"].(map[string]interface{})
	if count == nil {
		t.Fatal("sin count no puede paginar")
	}
	if _, hay := count["total"]; hay {
		t.Error("el recuento global de la plataforma no es asunto suyo")
	}
	if count["pages"] == nil {
		t.Error("necesita saber cuántas páginas hay para pasar a la siguiente")
	}
}
