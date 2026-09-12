package firma

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func registros(t *testing.T) *Registros {
	t.Helper()
	r, err := AbrirRegistros(t.TempDir())
	if err != nil {
		t.Fatalf("AbrirRegistros: %v", err)
	}
	t.Cleanup(func() { r.Cerrar() })
	return r
}

func TestRegistros_CicloDeUnaEmision(t *testing.T) {
	r := registros(t)

	// Lo que queda por hacer cuando llegue la firma viaja con el registro.
	espera, _ := json.Marshal(map[string]string{"to": "clave-del-beneficiario"})
	reg := &Registro{
		AssetID:      "asset-1",
		Operacion:    Emision,
		UserID:       "u1",
		Referencia:   "pagare-asset-1-emision",
		HashOriginal: "aaaa",
		Pendiente:    espera,
	}
	if err := r.Crear(reg, []byte("%PDF a firmar")); err != nil {
		t.Fatalf("Crear: %v", err)
	}
	if reg.ID == "" || reg.Estado != Pendiente || reg.CreadaAt.IsZero() {
		t.Fatalf("Crear no completó el registro: %+v", reg)
	}

	enEspera, err := r.EnEspera()
	if err != nil {
		t.Fatalf("EnEspera: %v", err)
	}
	if len(enEspera) != 1 || enEspera[0].ID != reg.ID {
		t.Fatalf("EnEspera = %d registros", len(enEspera))
	}
	var recuperada map[string]string
	if err := json.Unmarshal(enEspera[0].Pendiente, &recuperada); err != nil {
		t.Fatalf("la operación en espera no sobrevivió: %v", err)
	}
	if recuperada["to"] != "clave-del-beneficiario" {
		t.Errorf("la operación en espera se perdió: %v", recuperada)
	}

	if err := r.AnotarGUID(reg.ID, "GUID-1"); err != nil {
		t.Fatalf("AnotarGUID: %v", err)
	}

	pdf := []byte("%PDF-1.4 firmado")
	if err := r.Resolver(reg, &Firmado{PDF: pdf, Hash: "bbbb", HashOriginal: "aaaa"}); err != nil {
		t.Fatalf("Resolver: %v", err)
	}
	if reg.Estado != Firmada || reg.RutaPDF == "" || reg.ResueltaAt == nil {
		t.Fatalf("Resolver no cerró el registro: %+v", reg)
	}

	guardado, err := r.Ultima("asset-1")
	if err != nil {
		t.Fatalf("Ultima: %v", err)
	}
	if guardado.Estado != Firmada || guardado.HashFirmado != "bbbb" || guardado.GUID != "GUID-1" {
		t.Errorf("no se guardó lo resuelto: %+v", guardado)
	}
	if guardado.EnCurso() {
		t.Error("una firma resuelta ya no está en curso")
	}

	leido, err := r.PDFFirmado(guardado)
	if err != nil {
		t.Fatalf("PDFFirmado: %v", err)
	}
	if !bytes.Equal(leido, pdf) {
		t.Error("el PDF firmado no se recuperó igual")
	}
	if info, err := os.Stat(guardado.RutaPDF); err != nil {
		t.Errorf("el PDF no está en disco: %v", err)
	} else if info.Mode().Perm() != 0600 {
		t.Errorf("permisos del PDF firmado = %v, se esperaba 0600", info.Mode().Perm())
	}

	// Firmada pero con la operación sin ejecutar sigue en espera: el documento
	// está, lo que falta es llevarla a la cadena.
	if !guardado.AMedias() {
		t.Error("una firma resuelta sin ejecutar está a medias")
	}
	if enEspera, _ := r.EnEspera(); len(enEspera) != 1 {
		t.Errorf("una firma a medias tiene que seguir en espera: %d", len(enEspera))
	}

	// Ejecutada la operación, ya no hay nada que hacer con ella.
	if err := r.MarcarEjecutada(guardado); err != nil {
		t.Fatalf("MarcarEjecutada: %v", err)
	}
	if guardado.AMedias() {
		t.Error("ejecutada ya no está a medias")
	}
	final, _ := r.Ultima("asset-1")
	if final.EjecutadaAt == nil || final.AMedias() {
		t.Errorf("no se guardó la ejecución: %+v", final)
	}
	if enEspera, _ := r.EnEspera(); len(enEspera) != 0 {
		t.Errorf("una firma completa sigue en espera: %d", len(enEspera))
	}
}

// Una firma fallida no está a medias: no hay nada que reintentar.
func TestRegistros_FallidaNoEstaAMedias(t *testing.T) {
	r := registros(t)
	reg := &Registro{AssetID: "asset-3", Operacion: Emision, UserID: "u1",
		Referencia: "ref-3", HashOriginal: "aaaa"}
	if err := r.Crear(reg, []byte("%PDF a firmar")); err != nil {
		t.Fatal(err)
	}
	if err := r.Fallar(reg.ID, "Tiempo Expirado"); err != nil {
		t.Fatal(err)
	}
	guardado, _ := r.Ultima("asset-3")
	if guardado.AMedias() {
		t.Error("una firma fallida no está a medias")
	}
	if enEspera, _ := r.EnEspera(); len(enEspera) != 0 {
		t.Errorf("una firma fallida no tiene que estar en espera: %d", len(enEspera))
	}
}

// Un intento fallido no se borra: forma parte del historial del título.
func TestRegistros_FallarConservaElMotivo(t *testing.T) {
	r := registros(t)
	reg := &Registro{AssetID: "asset-2", Operacion: Endoso, UserID: "u1",
		Referencia: "ref-2", HashOriginal: "aaaa"}
	if err := r.Crear(reg, []byte("%PDF a firmar")); err != nil {
		t.Fatal(err)
	}
	if err := r.Fallar(reg.ID, "Chip NFC ilegible"); err != nil {
		t.Fatalf("Fallar: %v", err)
	}

	guardado, err := r.Ultima("asset-2")
	if err != nil {
		t.Fatal(err)
	}
	if guardado.Estado != Fallida || guardado.Motivo != "Chip NFC ilegible" {
		t.Errorf("no se guardó el fallo: %+v", guardado)
	}
	if guardado.EnCurso() {
		t.Error("una firma fallida no está en curso")
	}
}

// La referencia es única: es con lo que se consulta el envío en el portal, y
// dos firmas con la misma serían indistinguibles.
func TestRegistros_ReferenciaUnica(t *testing.T) {
	r := registros(t)
	uno := &Registro{AssetID: "a", Operacion: Emision, UserID: "u", Referencia: "misma", HashOriginal: "h"}
	otro := &Registro{AssetID: "b", Operacion: Emision, UserID: "u", Referencia: "misma", HashOriginal: "h"}
	if err := r.Crear(uno, []byte("%PDF")); err != nil {
		t.Fatal(err)
	}
	if err := r.Crear(otro, []byte("%PDF")); err == nil {
		t.Fatal("se admitió una referencia repetida")
	}
}

func TestRegistros_ConsultasSinResultado(t *testing.T) {
	r := registros(t)
	if _, err := r.Ultima("ninguno"); !errors.Is(err, ErrNoEncontrada) {
		t.Errorf("Ultima: se esperaba ErrNoEncontrada, se obtuvo %v", err)
	}
	if _, err := r.PorReferencia("ninguna"); !errors.Is(err, ErrNoEncontrada) {
		t.Errorf("PorReferencia: se esperaba ErrNoEncontrada, se obtuvo %v", err)
	}
	if _, err := r.PDFFirmado(nil); !errors.Is(err, ErrNoEncontrada) {
		t.Errorf("PDFFirmado: se esperaba ErrNoEncontrada, se obtuvo %v", err)
	}
}

func TestRegistros_CrearExigeLoMinimo(t *testing.T) {
	r := registros(t)
	for _, reg := range []*Registro{
		{Operacion: Emision, Referencia: "r", HashOriginal: "h"},
		{AssetID: "a", Operacion: Emision, HashOriginal: "h"},
		{AssetID: "a", Operacion: Emision, Referencia: "r"},
	} {
		if err := r.Crear(reg, []byte("%PDF a firmar")); err == nil {
			t.Errorf("se admitió un registro incompleto: %+v", reg)
		}
	}
}

// El documento que se mandó a firmar se conserva: es lo que un reintento
// vuelve a mandar, para que se firme lo mismo y no algo generado de nuevo.
func TestRegistros_ConservaElDocumentoAFirmar(t *testing.T) {
	r := registros(t)
	original := []byte("%PDF-1.3 el pagare tal como se mando a firmar")
	reg := &Registro{AssetID: "asset-4", Operacion: Emision, UserID: "u1",
		Referencia: "ref-4", HashOriginal: "aaaa"}
	if err := r.Crear(reg, original); err != nil {
		t.Fatalf("Crear: %v", err)
	}
	if reg.RutaOriginal == "" {
		t.Fatal("no se anotó la ruta del original")
	}

	guardado, err := r.Ultima("asset-4")
	if err != nil {
		t.Fatal(err)
	}
	leido, err := r.PDFOriginal(guardado)
	if err != nil {
		t.Fatalf("PDFOriginal: %v", err)
	}
	if !bytes.Equal(leido, original) {
		t.Error("el original no se recuperó igual")
	}

	// El firmado va a otro fichero: los dos tienen que convivir, porque el
	// original es lo que prueba qué se pidió firmar.
	if err := r.Resolver(guardado, &Firmado{PDF: []byte("%PDF firmado"), Hash: "bbbb"}); err != nil {
		t.Fatal(err)
	}
	if guardado.RutaPDF == guardado.RutaOriginal {
		t.Error("el firmado sobrescribió el original")
	}
	if leido, _ := r.PDFOriginal(guardado); !bytes.Equal(leido, original) {
		t.Error("el original se perdió al guardar el firmado")
	}
	if leido, _ := r.PDFFirmado(guardado); string(leido) != "%PDF firmado" {
		t.Error("el firmado no se guardó")
	}
}

// Sin documento no hay firma que pedir, así que no se abre el registro.
func TestRegistros_CrearExigeElDocumento(t *testing.T) {
	r := registros(t)
	reg := &Registro{AssetID: "a", Operacion: Emision, UserID: "u",
		Referencia: "ref-sin-doc", HashOriginal: "h"}
	if err := r.Crear(reg, nil); err == nil {
		t.Fatal("se admitió una firma sin documento")
	}
	if _, err := r.Ultima("a"); err == nil {
		t.Error("no debería haber quedado fila")
	}
}
