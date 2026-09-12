package scheduler

import (
	"context"
	"log"
	"time"
)

// Recogedor recoge las firmas de PDF ya firmadas y completa las operaciones que
// esperaban a ellas. Satisfecho por *handler.PagareHandler.
//
// Se declara aquí como interfaz para no arrastrar el paquete de los handlers
// hasta el cron: lo único que hace falta de él es este método.
type Recogedor interface {
	CompletarEnEspera(ctx context.Context) (revisadas, resueltas int, err error)
}

// RepasoFirmas recoge periódicamente las firmas pendientes.
//
// Hace falta porque nadie garantiza que el firmante vuelva a la aplicación
// después de firmar: sin esto, un pagaré firmado se queda sin entregar y un
// endoso firmado sin surtir efecto. Va aparte del chequeo de vencimientos y con
// su propio intervalo, porque una firma se espera en minutos y un vencimiento
// en días.
type RepasoFirmas struct {
	recogedor Recogedor
}

func NuevoRepasoFirmas(r Recogedor) *RepasoFirmas {
	return &RepasoFirmas{recogedor: r}
}

// Run repasa cada interval hasta que se cancele el contexto.
//
// No repasa al arrancar: un arranque no es un momento con más firmas listas
// que otro, y hacerlo retrasaría el servidor hablando con un portal externo
// justo cuando tiene que empezar a atender.
func (r *RepasoFirmas) Run(ctx context.Context, interval time.Duration) {
	if r.recogedor == nil {
		return
	}
	log.Printf("[cron] repaso de firmas cada %s", interval)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[cron] repaso de firmas detenido")
			return
		case <-t.C:
			r.Repasar(ctx)
		}
	}
}

// Repasar hace una pasada. Sólo deja rastro en el log cuando hay algo que
// contar: a un minuto de intervalo, anunciar cada pasada vacía sería enterrar
// el log en ruido.
func (r *RepasoFirmas) Repasar(ctx context.Context) {
	revisadas, resueltas, err := r.recogedor.CompletarEnEspera(ctx)
	if err != nil {
		log.Printf("[cron] repaso de firmas: %v", err)
		return
	}
	if revisadas > 0 {
		log.Printf("[cron] firmas: %d revisada(s), %d resuelta(s)", revisadas, resueltas)
	}
}
