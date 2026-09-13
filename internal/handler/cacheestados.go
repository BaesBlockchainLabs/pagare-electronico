package handler

import (
	"sync"
	"time"
)

// vidaEstado es cuánto se da por bueno el estado guardado de un pagaré.
//
// Corto a propósito. Las operaciones de la propia plataforma invalidan su
// pagaré en el acto, así que esto sólo acota lo que tarda en verse un cambio
// hecho desde fuera —otro cliente sobre el mismo libro—, y un minuto de retraso
// en un listado no engaña a nadie: lo que decide sigue siendo la cadena, que se
// consulta entera al abrir el pagaré.
const vidaEstado = time.Minute

// CacheEstados guarda el estado resuelto de cada pagaré durante un rato.
//
// Resolverlo cuesta una consulta al histórico por pagaré, así que pintar un
// listado costaba tantos viajes a la cadena como pagarés hubiera. Con esto, un
// listado que se repinta —o que se pagina hacia delante y hacia atrás— no los
// vuelve a pedir.
//
// El cero no sirve; usa NuevaCacheEstados. Una *CacheEstados nil se comporta
// como si no hubiera caché, para que quien no la tenga no se entere.
type CacheEstados struct {
	mu       sync.RWMutex
	vida     time.Duration
	entradas map[string]entradaEstado
}

type entradaEstado struct {
	estado string
	caduca time.Time
}

func NuevaCacheEstados() *CacheEstados {
	return &CacheEstados{vida: vidaEstado, entradas: make(map[string]entradaEstado)}
}

// Estado devuelve el estado guardado de un pagaré, si lo hay y sigue fresco.
func (c *CacheEstados) Estado(assetID string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.RLock()
	e, hay := c.entradas[assetID]
	c.mu.RUnlock()
	if !hay || time.Now().After(e.caduca) {
		return "", false
	}
	return e.estado, true
}

// Guardar anota el estado de un pagaré.
func (c *CacheEstados) Guardar(assetID, estado string) {
	if c == nil || assetID == "" {
		return
	}
	c.mu.Lock()
	c.entradas[assetID] = entradaEstado{estado: estado, caduca: time.Now().Add(c.vida)}
	c.mu.Unlock()
}

// Olvidar tira el estado guardado de un pagaré. Lo llaman las operaciones que
// lo cambian, para que el listado no siga enseñando el anterior durante un
// minuto después de endosarlo.
func (c *CacheEstados) Olvidar(assetID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.entradas, assetID)
	c.mu.Unlock()
}

// Vaciar tira todo lo guardado.
func (c *CacheEstados) Vaciar() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entradas = make(map[string]entradaEstado)
	c.mu.Unlock()
}
