package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type recogedorFalso struct {
	pasadas   atomic.Int32
	revisadas int
	resueltas int
	err       error
}

func (r *recogedorFalso) CompletarEnEspera(context.Context) (int, int, error) {
	r.pasadas.Add(1)
	return r.revisadas, r.resueltas, r.err
}

// El repaso vuelve a pasar en cada tic y no se detiene por un error: el portal
// puede estar caído un rato y las firmas siguen ahí.
func TestRepasoFirmas_RepiteYSobreviveAlError(t *testing.T) {
	r := &recogedorFalso{revisadas: 2, resueltas: 1, err: errors.New("portal caído")}
	repaso := NuevoRepasoFirmas(r)

	ctx, cancelar := context.WithCancel(context.Background())
	hecho := make(chan struct{})
	go func() { repaso.Run(ctx, 10*time.Millisecond); close(hecho) }()

	deadline := time.After(2 * time.Second)
	for r.pasadas.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("sólo %d pasada(s) en 2 s", r.pasadas.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancelar()
	select {
	case <-hecho:
	case <-time.After(time.Second):
		t.Fatal("el repaso no se detuvo al cancelar el contexto")
	}
}

// Sin recogedor no hay nada que repasar, y arrancarlo no puede reventar.
func TestRepasoFirmas_SinRecogedor(t *testing.T) {
	hecho := make(chan struct{})
	go func() { NuevoRepasoFirmas(nil).Run(context.Background(), time.Millisecond); close(hecho) }()
	select {
	case <-hecho:
	case <-time.After(time.Second):
		t.Fatal("sin recogedor tiene que volver en el acto")
	}
}

// Una pasada suelta es lo que usa el arranque manual.
func TestRepasoFirmas_Repasar(t *testing.T) {
	r := &recogedorFalso{revisadas: 1, resueltas: 1}
	NuevoRepasoFirmas(r).Repasar(context.Background())
	if r.pasadas.Load() != 1 {
		t.Errorf("pasadas = %d, se esperaba 1", r.pasadas.Load())
	}
}
