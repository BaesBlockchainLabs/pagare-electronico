package handler

import (
	"testing"
	"time"
)

func TestCacheEstados(t *testing.T) {
	c := NuevaCacheEstados()

	if _, hay := c.Estado("asset-1"); hay {
		t.Error("una caché nueva no puede saber nada")
	}

	c.Guardar("asset-1", "ACTIVO")
	if estado, hay := c.Estado("asset-1"); !hay || estado != "ACTIVO" {
		t.Errorf("estado = %q, hay = %v", estado, hay)
	}

	// Olvidar es lo que hacen las operaciones que cambian el pagaré: sin esto,
	// el listado seguiría enseñando el estado anterior hasta que caducara.
	c.Olvidar("asset-1")
	if _, hay := c.Estado("asset-1"); hay {
		t.Error("olvidada, no puede seguir ahí")
	}

	c.Guardar("asset-2", "PAGADO")
	c.Vaciar()
	if _, hay := c.Estado("asset-2"); hay {
		t.Error("vaciada, no puede quedar nada")
	}
}

// Lo guardado caduca solo, que es lo que acota ver un cambio hecho desde fuera.
func TestCacheEstados_Caduca(t *testing.T) {
	c := NuevaCacheEstados()
	c.vida = 20 * time.Millisecond
	c.Guardar("asset-1", "ACTIVO")

	if _, hay := c.Estado("asset-1"); !hay {
		t.Fatal("recién guardada tiene que estar")
	}
	time.Sleep(40 * time.Millisecond)
	if _, hay := c.Estado("asset-1"); hay {
		t.Error("caducada, no puede seguir sirviéndose")
	}
}

// Una caché nil se comporta como si no la hubiera: quien no la tenga conectada
// no se entera, y ninguna de sus llamadas revienta.
func TestCacheEstados_Nil(t *testing.T) {
	var c *CacheEstados
	if _, hay := c.Estado("asset-1"); hay {
		t.Error("una caché nil no sabe nada")
	}
	c.Guardar("asset-1", "ACTIVO")
	c.Olvidar("asset-1")
	c.Vaciar()
}

// Se usa desde varias peticiones a la vez y desde la resolución en paralelo.
func TestCacheEstados_UsoConcurrente(t *testing.T) {
	c := NuevaCacheEstados()
	hecho := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(i int) {
			for j := 0; j < 50; j++ {
				c.Guardar("asset", "ACTIVO")
				c.Estado("asset")
				if j%10 == 0 {
					c.Olvidar("asset")
				}
			}
			hecho <- struct{}{}
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-hecho
	}
}
