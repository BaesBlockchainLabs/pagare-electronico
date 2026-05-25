// Package scheduler contiene el chequeo periódico de pagarés que sobrepasan su
// fecha de validez: vencidos (fecha fija), caducados a la vista (art. 39 LCCH)
// y prescritos (3 años desde la emisión, art. 88 LCCH).
//
// El chequeo es de solo lectura: NO muta la blockchain. Genera un índice de
// alertas consultable; las acciones (pago, anulación, prescripción) quedan en
// manos de una persona.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"pagare/internal/bcfclient"
)

const dateLayout = "2006-01-02"

// Categorías de alerta.
const (
	CatVencido       = "VENCIDO"
	CatCaducadoVista = "CADUCADO_VISTA"
	CatPrescrito     = "PRESCRITO"
)

// Alerta describe un pagaré que ha sobrepasado un plazo legal.
type Alerta struct {
	ID           string `json:"id"`
	Categoria    string `json:"categoria"`
	Mensaje      string `json:"mensaje"`
	ArticuloLCCH string `json:"articulo_lcch,omitempty"`
	Beneficiario string `json:"beneficiario,omitempty"`
	Importe      string `json:"importe,omitempty"`
	Fecha        string `json:"fecha,omitempty"`
}

// Checker consulta la blockchain periódicamente y mantiene el índice de alertas.
type Checker struct {
	client *bcfclient.Client

	mu      sync.RWMutex
	alertas []Alerta
	lastRun time.Time
}

func NewChecker(client *bcfclient.Client) *Checker {
	return &Checker{client: client, alertas: []Alerta{}}
}

// Run ejecuta un chequeo inmediato y luego uno cada interval, hasta que se
// cancele el contexto.
func (c *Checker) Run(ctx context.Context, interval time.Duration) {
	log.Printf("[cron] chequeo de vencimientos cada %s", interval)
	c.Check()

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[cron] detenido")
			return
		case <-t.C:
			c.Check()
		}
	}
}

// Alertas devuelve una copia del índice actual y la hora del último chequeo.
func (c *Checker) Alertas() ([]Alerta, time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Alerta, len(c.alertas))
	copy(out, c.alertas)
	return out, c.lastRun
}

// Check recorre todos los pagarés y recalcula el índice de alertas.
func (c *Checker) Check() {
	now := time.Now()
	alertas, err := c.scan(now)
	if err != nil {
		log.Printf("[cron] error en el chequeo: %v", err)
		return
	}
	c.mu.Lock()
	c.alertas = alertas
	c.lastRun = now
	c.mu.Unlock()
	log.Printf("[cron] chequeo completado: %d alertas", len(alertas))
}

func (c *Checker) scan(now time.Time) ([]Alerta, error) {
	query := map[string]interface{}{
		"data": map[string]string{"type": "pagare_electronico"},
	}
	body, status, err := c.client.GetAsset(query)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("blockchain status %d", status)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	assets, _ := raw["assets"].([]interface{})
	alertas := []Alerta{}
	for _, a := range assets {
		asset, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := asset["id"].(string)
		data, _ := asset["data"].(map[string]interface{})
		if data == nil {
			continue
		}

		// Los pagarés ya cerrados (pagados/anulados/prescritos = quemados) no generan alerta.
		if id != "" && c.cerrado(id) {
			continue
		}

		tipoVenc, fechaVenc := "", ""
		if ven, ok := data["vencimiento"].(map[string]interface{}); ok {
			tipoVenc, _ = ven["tipo"].(string)
			fechaVenc, _ = ven["fecha"].(string)
		}
		fechaEmision, _ := data["fecha_emision"].(string)

		cat, msg, art := Clasificar(tipoVenc, fechaVenc, fechaEmision, now)
		if cat == "" {
			continue
		}
		alertas = append(alertas, Alerta{
			ID:           id,
			Categoria:    cat,
			Mensaje:      msg,
			ArticuloLCCH: art,
			Beneficiario: beneficiarioStr(data),
			Importe:      importeStr(data),
			Fecha:        relevantDate(cat, tipoVenc, fechaVenc, fechaEmision),
		})
	}
	return alertas, nil
}

// cerrado devuelve true si el histórico del asset contiene una quema o un cierre.
func (c *Checker) cerrado(id string) bool {
	body, status, err := c.client.GetAssetHistory(id)
	if err != nil || status != 200 {
		return false
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	history, _ := raw["history"].([]interface{})
	for _, e := range history {
		entry, _ := e.(map[string]interface{})
		meta, _ := entry["metadata"].(map[string]interface{})
		if meta == nil {
			continue
		}
		if action, _ := meta["action"].(string); action == "BURN" {
			return true
		}
		if _, ok := meta["tipo_cierre"]; ok {
			return true
		}
	}
	return false
}

// Clasificar determina la categoría de alerta de un pagaré según sus fechas.
// Devuelve cadena vacía si el pagaré está en plazo. Es una función pura.
//
// Orden de prioridad (de más grave a menos):
//  1. PRESCRITO: 3 años desde la emisión (art. 88 LCCH).
//  2. CADUCADO_VISTA: pagaré a la vista no presentado en 1 año (art. 39 LCCH).
//  3. VENCIDO: superada la fecha fija de vencimiento.
func Clasificar(tipoVenc, fechaVenc, fechaEmision string, now time.Time) (categoria, mensaje, articulo string) {
	if em, err := time.Parse(dateLayout, fechaEmision); err == nil {
		if !now.Before(em.AddDate(3, 0, 0)) {
			return CatPrescrito, "Han transcurrido 3 años desde la emisión: el pagaré puede haber prescrito.", "art. 88 LCCH"
		}
		if tipoVenc == "a_la_vista" && !now.Before(em.AddDate(1, 0, 0)) {
			return CatCaducadoVista, "Pagaré a la vista: superado el plazo de presentación de 1 año desde la emisión.", "art. 39 LCCH"
		}
	}
	if tipoVenc != "a_la_vista" && fechaVenc != "" {
		if v, err := time.Parse(dateLayout, fechaVenc); err == nil && now.After(v) {
			return CatVencido, "Vencido: se ha superado la fecha de vencimiento.", ""
		}
	}
	return "", "", ""
}

func relevantDate(cat, tipoVenc, fechaVenc, fechaEmision string) string {
	switch cat {
	case CatVencido:
		return fechaVenc
	default:
		return fechaEmision
	}
}

func beneficiarioStr(data map[string]interface{}) string {
	ben, _ := data["beneficiario"].(map[string]interface{})
	if ben == nil {
		return ""
	}
	nombre, _ := ben["nombre"].(string)
	apellido, _ := ben["apellido"].(string)
	if apellido != "" {
		return nombre + " " + apellido
	}
	return nombre
}

func importeStr(data map[string]interface{}) string {
	moneda, _ := data["moneda"].(string)
	if moneda == "" {
		moneda = "EUR"
	}
	switch v := data["importe"].(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', 2, 64) + " " + moneda
	case string:
		return v + " " + moneda
	default:
		return ""
	}
}
