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
	if err := r.Crear(reg); err != nil {
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

	// Resuelta, ya no aparece entre las que hay que consultar al portal.
	if enEspera, _ := r.EnEspera(); len(enEspera) != 0 {
		t.Errorf("una firma resuelta sigue en espera: %d", len(enEspera))
	}
}

// Un intento fallido no se borra: forma parte del historial del título.
func TestRegistros_FallarConservaElMotivo(t *testing.T) {
	r := registros(t)
	reg := &Registro{AssetID: "asset-2", Operacion: Endoso, UserID: "u1",
		Referencia: "ref-2", HashOriginal: "aaaa"}
	if err := r.Crear(reg); err != nil {
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
	if err := r.Crear(uno); err != nil {
		t.Fatal(err)
	}
	if err := r.Crear(otro); err == nil {
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
		if err := r.Crear(reg); err == nil {
			t.Errorf("se admitió un registro incompleto: %+v", reg)
		}
	}
}
