package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"pagare/internal/auth"
	"pagare/internal/bcfclient"
	"pagare/internal/models"
	"pagare/internal/pdf"
)

// UserResolver resolves a blockchain public key to its registered user, so the
// PDF can show real names/NIF for participants known only by their pub in the
// asset history. Satisfied by *auth.Store.
type UserResolver interface {
	GetUserByPubKey(pub string) (*auth.User, error)
}

// SignatureVerifier checks a signed message against a public key, returning the
// message that was signed. Satisfied by *crypto.Service.
type SignatureVerifier interface {
	VerifyPagareSignature(signedMessage, verifyKey string) (string, error)
}

type ConsultaHandler struct {
	// estados guarda el estado resuelto de cada pagaré, que cuesta una consulta
	// al histórico. Nil se comporta como si no hubiera caché.
	estados *CacheEstados

	client       *bcfclient.Client
	users        UserResolver
	crypto       SignatureVerifier
	certificador Certificador
}

func NewConsultaHandler(client *bcfclient.Client) *ConsultaHandler {
	return &ConsultaHandler{client: client}
}

// SetCacheEstados conecta la caché de estados. Sin ella el listado funciona
// igual, sólo que preguntando al libro cada vez.
func (h *ConsultaHandler) SetCacheEstados(c *CacheEstados) { h.estados = c }

// SetUsers wires the user resolver used to enrich the PDF from public keys.
func (h *ConsultaHandler) SetUsers(u UserResolver) { h.users = u }

// SetCrypto wires the verifier used to check the firmante's signature.
func (h *ConsultaHandler) SetCrypto(c SignatureVerifier) { h.crypto = c }

// resolveUser returns the registered user for a pub, or nil if unknown.
func (h *ConsultaHandler) resolveUser(pub string) *auth.User {
	if h.users == nil || pub == "" {
		return nil
	}
	u, err := h.users.GetUserByPubKey(pub)
	if err != nil {
		return nil
	}
	return u
}

func (h *ConsultaHandler) ListPagares(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	query := map[string]interface{}{
		"data": map[string]string{"type": "pagare_electronico"},
	}
	if q := r.URL.Query().Get("query"); q != "" {
		var customQuery map[string]interface{}
		if err := json.Unmarshal([]byte(q), &customQuery); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "Query JSON inválido"})
			return
		}
		query = customQuery
	}
	// Paginar no es cosmética: resolver el estado de un pagaré cuesta una
	// consulta al histórico, así que traerlos todos costaba tantos viajes a la
	// cadena como pagarés hubiera, y crecía con el uso. El libro los devuelve
	// del más antiguo al más nuevo, e inverse los da al revés: en una cartera
	// interesa lo último.
	aplicarPaginacion(query, r)

	body, status, err := h.client.GetAsset(query)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	if status != 200 {
		WriteRaw(w, status, body)
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		WriteRaw(w, status, body)
		return
	}

	assets, _ := raw["assets"].([]interface{})
	actionToEstado := map[string]string{
		"PAGO": "PAGADO", "ANULACION": "ANULADO", "PRESCRIPCION": "PRESCRITO",
		"ENDOSO": "ENDOSADO",
	}

	// El estado y la titularidad de cada pagaré se resuelven en paralelo y de
	// una sola lectura del histórico: son consultas independientes que en serie
	// sumaban su latencia una tras otra.
	datos := h.datosDeLaPagina(assets, principal, actionToEstado)

	for _, a := range assets {
		asset, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		assetID, _ := asset["id"].(string)
		estado := datos[assetID].estado
		data, _ := asset["data"].(map[string]interface{})
		if data == nil {
			data = make(map[string]interface{})
		}
		if estado == "" {
			ven, _ := data["vencimiento"].(map[string]interface{})
			if ven != nil {
				tipo, _ := ven["tipo"].(string)
				fechaStr, _ := ven["fecha"].(string)
				if tipo != "a_la_vista" && fechaStr != "" {
					if t, err := time.Parse("2006-01-02", fechaStr); err == nil && t.Before(time.Now()) {
						estado = "VENCIDO"
					}
				}
			}
		}
		if estado != "" {
			data["estado"] = estado
			asset["data"] = data
		}
	}

	// Apply ownership scoping for regular users (admins see everything "tal cual")
	h.filtrarPorTitularidad(raw, principal, datos)

	WriteJSON(w, status, raw)
}

// paginacionPorDefecto es cuántos pagarés trae una página cuando no se pide
// otra cosa. Es el mismo que usa el libro por su cuenta.
const paginacionPorDefecto = 25

// aplicarPaginacion traslada a la consulta del libro la página que se pide, y
// deja el orden del más nuevo al más antiguo salvo que se diga lo contrario.
func aplicarPaginacion(query map[string]interface{}, r *http.Request) {
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 0 {
		query["page_num"] = n
	} else if _, ya := query["page_num"]; !ya {
		query["page_num"] = 1
	}

	if n, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil && n > 0 {
		// Un tope: la página la pide el cliente y sin límite volveríamos a
		// traerlo todo, que es justo lo que se quiere evitar.
		if n > 100 {
			n = 100
		}
		query["per_page"] = n
	} else if _, ya := query["per_page"]; !ya {
		query["per_page"] = paginacionPorDefecto
	}

	if _, ya := query["inverse"]; !ya {
		query["inverse"] = true
	}
}

// datosPagare es lo que el listado necesita saber de la cadena sobre cada
// pagaré: en qué estado está y si es del usuario que mira.
type datosPagare struct {
	estado string
	mio    bool
}

// datosDeLaPagina reúne esos datos para todos los pagarés de una página, en
// paralelo y pidiendo cada cosa una sola vez.
//
// Antes costaba hasta tres viajes a la cadena por pagaré, en serie: uno para el
// estado, otro para los propietarios y otro más para el histórico —el mismo que
// ya se había pedido para el estado— cuando el usuario no figuraba como dueño.
// Ahora el histórico se pide una vez y de él salen las dos respuestas.
//
// El paralelismo va acotado: son llamadas a un servicio ajeno, y soltarle
// veinticinco de golpe no es ser mejor vecino que hacerlo de una en una es ser
// razonable con nosotros mismos.
func (h *ConsultaHandler) datosDeLaPagina(assets []interface{}, p *auth.Principal,
	actionMap map[string]string) map[string]datosPagare {

	const enParalelo = 8

	ids := make([]string, 0, len(assets))
	for _, a := range assets {
		asset, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := asset["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}

	datos := make(map[string]datosPagare, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	hueco := make(chan struct{}, enParalelo)

	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			hueco <- struct{}{}
			defer func() { <-hueco }()

			estado, enCache := h.estados.Estado(id)

			// A un administrador no hay que resolverle la titularidad: los ve
			// todos igual, así que preguntarla sería una consulta por pagaré
			// tirada a la basura.
			mio := p == nil || p.IsAdmin()
			if !mio {
				// Figurar como propietario es el criterio barato y el habitual,
				// así que se prueba primero: si acierta y el estado ya estaba en
				// caché, este pagaré cuesta una sola consulta.
				mio = h.figuraComoPropietario(id, p)
			}

			var historial []interface{}
			if !enCache || !mio {
				historial = h.historialDe(id)
			}
			if !enCache {
				estado = estadoDesdeHistorial(historial, actionMap)
				h.estados.Guardar(id, estado)
			}
			if !mio {
				mio = apareceEnHistorial(historial, p)
			}

			mu.Lock()
			datos[id] = datosPagare{estado: estado, mio: mio}
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	return datos
}

// historialDe lee el histórico de un pagaré. Se separa de interpretarlo porque
// el listado saca dos cosas del mismo histórico —el estado y si el pagaré es
// del usuario— y pedirlo dos veces era la mitad de su lentitud.
func (h *ConsultaHandler) historialDe(assetID string) []interface{} {
	body, status, err := h.client.GetAssetHistory(assetID)
	if err != nil || status != 200 {
		return nil
	}
	var raw map[string]interface{}
	if json.Unmarshal(body, &raw) != nil {
		return nil
	}
	history, _ := raw["history"].([]interface{})
	return history
}

func (h *ConsultaHandler) resolveEstado(assetID string, actionMap map[string]string) string {
	return estadoDesdeHistorial(h.historialDe(assetID), actionMap)
}

func estadoDesdeHistorial(history []interface{}, actionMap map[string]string) string {
	var cierre, transfer, update string
	var hasBurn, hasTransfer bool
	for i := len(history) - 1; i >= 0; i-- {
		entry, _ := history[i].(map[string]interface{})
		metadata, _ := entry["metadata"].(map[string]interface{})
		if metadata == nil {
			continue
		}
		if tipo, ok := metadata["tipo_cierre"].(string); ok {
			if estado, found := actionMap[tipo]; found && cierre == "" {
				cierre = estado
			}
		}
		action, _ := metadata["action"].(string)
		// La entrega y la cesión se registran como TRANSFER, igual que un
		// endoso, pero no lo son: un pagaré recién emitido no está endosado, y
		// uno cedido lo fue por cesión ordinaria, con otro régimen.
		if action == "TRANSFER" {
			hasTransfer = true
		}
		if action == "TRANSFER" && transfer == "" && !esEntrega(metadata) {
			if esCesion(metadata) {
				transfer = "CEDIDO"
			} else {
				transfer = "ENDOSADO"
			}
		}
		if action == "ENDOSO" && update == "" {
			update = "ENDOSADO"
		}
		if action == "BURN" {
			hasBurn = true
		}
		if hasBurn && cierre == "" {
			if estado, found := actionMap[action]; found {
				cierre = estado
			}
		}
	}
	if cierre != "" {
		return cierre
	}
	if hasBurn {
		return "PAGADO"
	}
	if transfer != "" {
		return transfer
	}
	if update != "" {
		return update
	}
	// Sin transferencia alguna el control sigue en el emisor: el pagaré está
	// firmado pero no ha llegado a manos de su beneficiario, y hasta que llegue
	// éste no tiene nada. Conviene que se vea.
	if !hasTransfer {
		return "PENDIENTE_ENTREGA"
	}
	return ""
}

func (h *ConsultaHandler) GetPagare(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}

	// Regular users can only access assets they own
	if !principal.IsAdmin() && !h.assetOwnedBy(id, principal) {
		WriteJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "msg": "no tienes acceso a este pagaré"})
		return
	}

	body, status, err := h.client.GetAsset(map[string]string{"id": id})
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *ConsultaHandler) GetHistorico(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}

	// Regular users can only see history of assets they own
	if !principal.IsAdmin() && !h.assetOwnedBy(id, principal) {
		WriteJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "msg": "no tienes acceso a este pagaré"})
		return
	}

	body, status, err := h.client.GetAssetHistory(id)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	if status != 200 {
		WriteRaw(w, status, body)
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		WriteRaw(w, status, body)
		return
	}

	actionLabels := map[string]string{
		"CREATE": "Emisión", "UPDATE": "Actualización", "TRANSFER": "Endoso (transferencia)",
		"BURN": "Quema", "ENDOSO": "Endoso",
		"PAGO": "Pago", "ANULACION": "Anulación", "PRESCRIPCION": "Prescripción",
	}

	history, _ := raw["history"].([]interface{})
	for _, entry := range history {
		e, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		metadata, _ := e["metadata"].(map[string]interface{})
		if metadata == nil {
			continue
		}
		action, _ := metadata["action"].(string)
		switch {
		case esEntrega(metadata):
			metadata["action_label"] = "Entrega al beneficiario"
		case esCesion(metadata):
			metadata["action_label"] = "Cesión ordinaria (arts. 347-348 CCom)"
		default:
			if label, found := actionLabels[action]; found {
				metadata["action_label"] = label
			} else {
				metadata["action_label"] = action
			}
		}
		if tipo, ok := metadata["tipo_cierre"].(string); ok {
			if label, found := actionLabels[tipo]; found {
				metadata["tipo_cierre_label"] = label
			}
		}
		if tipo, ok := metadata["tipo_endoso"].(string); ok {
			endosoLabels := map[string]string{
				"en_propiedad":   "En propiedad (art. 17)",
				"en_procuracion": "En procuración (art. 21)",
				"en_blanco":      "En blanco (art. 15)",
				"en_garantia":    "En garantía / prenda (art. 22)",
			}
			if label, found := endosoLabels[tipo]; found {
				metadata["tipo_endoso_label"] = label
			}
		}
		if clausula, ok := metadata["clausula"].(string); ok {
			clausulaLabels := map[string]string{
				"sin_responsabilidad": "Sin mi responsabilidad (art. 18)",
				"no_a_la_orden":       "Prohibición de nuevo endoso (art. 18)",
				"sin_gastos":          "Sin gastos / sin protesto (art. 56)",
			}
			if label, found := clausulaLabels[clausula]; found {
				metadata["clausula_label"] = label
			}
		}
		if ts, ok := metadata["updated_at"].(float64); ok {
			metadata["fecha"] = time.UnixMilli(int64(ts)).Format("2006-01-02 15:04:05")
		}
		if ts, ok := metadata["fecha_pago"].(string); ok {
			metadata["fecha"] = ts
		}
		if _, ok := metadata["fecha"]; !ok {
			if ts, ok := metadata["updated_at"].(float64); ok {
				metadata["fecha"] = time.UnixMilli(int64(ts)).Format("2006-01-02 15:04:05")
			}
		}
	}

	WriteJSON(w, status, raw)
}

func (h *ConsultaHandler) GetPropietario(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}

	// Only owners (or admins) can query current owners of an asset
	if !principal.IsAdmin() && !h.assetOwnedBy(id, principal) {
		WriteJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "msg": "no tienes acceso a este pagaré"})
		return
	}

	body, status, err := h.client.GetAssetOwners(id)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *ConsultaHandler) GetPublicAsset(w http.ResponseWriter, r *http.Request) {
	network := r.URL.Query().Get("network")
	id := r.URL.Query().Get("id")
	if network == "" || id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "network e id son obligatorios"})
		return
	}

	body, status, err := h.client.GetPublicAsset(network, id)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	if status != 200 {
		WriteRaw(w, status, body)
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		WriteRaw(w, status, body)
		return
	}

	asset, _ := raw["asset"].(map[string]interface{})
	if asset == nil {
		WriteRaw(w, status, body)
		return
	}

	estado := h.resolveEstado(id, map[string]string{
		"PAGO": "PAGADO", "ANULACION": "ANULADO", "PRESCRIPCION": "PRESCRITO",
		"ENDOSO": "ENDOSADO",
	})
	data, _ := asset["data"].(map[string]interface{})
	if data == nil {
		data = make(map[string]interface{})
	}
	if estado == "" {
		ven, _ := data["vencimiento"].(map[string]interface{})
		if ven != nil {
			tipo, _ := ven["tipo"].(string)
			fechaStr, _ := ven["fecha"].(string)
			if tipo != "a_la_vista" && fechaStr != "" {
				if t, err := time.Parse("2006-01-02", fechaStr); err == nil && t.Before(time.Now()) {
					estado = "VENCIDO"
				}
			}
		}
	}
	if estado != "" {
		data["estado"] = estado
	}

	// La verificación se calcula sobre el contenido completo: es lo que se
	// firmó. Sólo después se recorta lo que se devuelve.
	verificacion := h.verificarContenido(data)

	interesado := h.esInteresado(id, auth.GetPrincipal(r))
	if !interesado {
		data = vistaPublica(data)
	}

	ownerBody, ownerStatus, _ := h.client.GetAssetOwners(id)
	if interesado && ownerStatus == 200 {
		var ownerRaw map[string]interface{}
		if json.Unmarshal(ownerBody, &ownerRaw) == nil {
			if owners, ok := ownerRaw["owners"].([]interface{}); ok && len(owners) > 0 {
				if first, ok := owners[0].(map[string]interface{}); ok {
					if pub, ok := first["pub"].(string); ok {
						data["propietario_actual"] = pub
					}
				}
			}
		}
	}

	asset["data"] = data
	asset["verificacion"] = verificacion
	// Que la vista esté recortada debe verse, no adivinarse.
	asset["vista"] = "publica"
	if interesado {
		asset["vista"] = "completa"
	}

	WriteJSON(w, status, raw)
}

// filterForPrincipal removes assets from the response that the non-admin principal
// does not own (based on current pubkey owners from BCF). Admins see the full set.
// filtrarPorTitularidad deja sólo los pagarés del usuario, usando la
// titularidad ya resuelta para la página: volver a preguntarla aquí duplicaría
// las consultas a la cadena.
//
// El filtro es posterior a la paginación porque el libro no sabe filtrar por
// propietario —su consulta admite id, data, fechas y paginación, nada más—, así
// que una página puede quedarse con menos pagarés de los que trajo, o con
// ninguno. Es la contrapartida de no traerlos todos, y se prefiere: traerlos
// todos crece sin tope y acaba por no funcionar para nadie.
func (h *ConsultaHandler) filtrarPorTitularidad(raw map[string]interface{},
	principal *auth.Principal, datos map[string]datosPagare) {

	if principal == nil || principal.IsAdmin() {
		return
	}

	assetsIface, _ := raw["assets"].([]interface{})
	if assetsIface == nil {
		return
	}

	filtrados := make([]interface{}, 0, len(assetsIface))
	for _, a := range assetsIface {
		asset, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := asset["id"].(string)
		if id != "" && datos[id].mio {
			filtrados = append(filtrados, a)
		}
	}
	raw["assets"] = filtrados

	// El recuento global no es asunto suyo —cuántos pagarés hay en la
	// plataforma no le incumbe—, pero sin saber cuántas páginas hay no puede
	// pasar a la siguiente. Se le deja lo segundo y se le quita lo primero.
	if count, ok := raw["count"].(map[string]interface{}); ok {
		delete(count, "total")
	}
}

// assetOwnedBy returns true if any of the current owners (by pubkey) matches
// one of the principal's claimed pubkeys.
// DescargarPDF renders the pagaré (anverso + reverso) as a downloadable PDF.
// Access is restricted to the owner (or an admin), reusing the ownership rule.
func (h *ConsultaHandler) DescargarPDF(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}
	if !principal.IsAdmin() && !h.assetOwnedBy(id, principal) {
		WriteJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "msg": "no tienes acceso a este pagaré"})
		return
	}

	assetBody, status, err := h.client.GetAsset(map[string]string{"id": id})
	if err != nil || status != 200 {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": "no se pudo obtener el pagaré"})
		return
	}
	pagare, err := assetToPagare(assetBody)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "datos del pagaré no válidos"})
		return
	}

	var endosos []pdf.Endoso
	var cesiones []pdf.Cesion
	var firmantePub string
	if histBody, hs, herr := h.client.GetAssetHistory(id); herr == nil && hs == 200 {
		endosos, cesiones, firmantePub = h.parseHistoryCompleto(histBody)
	}
	estado := h.resolveEstado(id, map[string]string{
		"PAGO": "PAGADO", "ANULACION": "ANULADO", "PRESCRIPCION": "PRESCRITO",
	})

	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	verifyURL := fmt.Sprintf("%s://%s/pagares/verificar?network=test&id=%s", scheme, r.Host, id)

	out, err := pdf.Generate(pdf.Input{P: pagare, AssetID: id, VerifyURL: verifyURL, FirmantePub: firmantePub, Estado: estado, Endosos: endosos, Cesiones: cesiones})
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "no se pudo generar el PDF"})
		return
	}

	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pagare-%s.pdf"`, short))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// assetToPagare reconstructs a PagareElectronico from a BCF GetAsset response
// (the stored data mirrors the model's JSON tags).
func assetToPagare(body []byte) (*models.PagareElectronico, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if a, ok := raw["asset"].(map[string]interface{}); ok {
		data, _ = a["data"].(map[string]interface{})
	}
	if data == nil {
		data, _ = raw["data"].(map[string]interface{})
	}
	if data == nil {
		return nil, fmt.Errorf("sin datos de pagaré")
	}
	return pagareFromData(data)
}

// parseHistory extracts, in chronological order, the endorsement chain and the
// firmante's signing public key from a BCF GetAssetHistory response. Endosatario
// identities are resolved from their public key (`to`) against the user store,
// since the history metadata carries only the keys, not the names.
func (h *ConsultaHandler) parseHistory(body []byte) (endosos []pdf.Endoso, firmantePub string) {
	endosos, _, firmantePub = h.parseHistoryCompleto(body)
	return endosos, firmantePub
}

// parseHistoryCompleto also returns the assignments, which travel apart from
// the endorsement chain because they bind the cedente to a different régimen.
func (h *ConsultaHandler) parseHistoryCompleto(body []byte) (endosos []pdf.Endoso, cesiones []pdf.Cesion, firmantePub string) {
	var raw map[string]interface{}
	if json.Unmarshal(body, &raw) != nil {
		return nil, nil, ""
	}
	hist, _ := raw["history"].([]interface{})
	for _, item := range hist {
		e, _ := item.(map[string]interface{})
		if e == nil {
			continue
		}
		meta, _ := e["metadata"].(map[string]interface{})
		if meta == nil {
			continue
		}
		action := strVal(meta["action"])
		tipo := strVal(meta["tipo_endoso"])

		if action == "CREATE" {
			firmantePub = strVal(meta["from"]) // la clave que firmó la emisión
			continue
		}
		if esEntrega(meta) {
			continue // la entrega al beneficiario no abre la cadena de endosos
		}
		if esCesion(meta) {
			// La cesión transmite bajo otro régimen, sin responsabilidad del
			// cedente por la solvencia: va aparte, no en la cadena de endosos.
			c := pdf.Cesion{
				CedentePub:        strVal(meta["from"]),
				NotificacionFecha: strVal(meta["notificacion_fecha"]),
				NotificacionMedio: strVal(meta["notificacion_medio"]),
			}
			if f := strVal(meta["fecha"]); len(f) >= 10 {
				c.Fecha = f[:10]
			}
			if ce, ok := meta["cesionario"].(map[string]interface{}); ok {
				c.Cesionario = strings.TrimSpace(strVal(ce["nombre"]) + " " + strVal(ce["apellido"]))
				c.NIF = strVal(ce["nif"])
			}
			toPub := strVal(meta["to"])
			if c.Cesionario == "" && toPub != "" {
				if u := h.resolveUser(toPub); u != nil {
					c.Cesionario = strings.TrimSpace(u.Nombre + " " + u.Apellido)
					c.NIF = u.NIF
				} else {
					c.Cesionario = "clave " + shortIDStr(toPub)
				}
			}
			cesiones = append(cesiones, c)
			continue
		}
		if action != "TRANSFER" && action != "ENDOSO" && tipo == "" {
			continue // UPDATE/BURN u otros: no son endosos
		}

		end := pdf.Endoso{
			Tipo:         tipo,
			Clausula:     strVal(meta["clausula"]),
			EndosantePub: strVal(meta["from"]),
		}
		if f := strVal(meta["fecha"]); len(f) >= 10 {
			end.Fecha = f[:10] // "2026-07-26 22:27:55" o RFC3339 -> "2026-07-26"
		}

		// Endosatario: primero por metadata (si viniera), si no por su pub.
		if en, ok := meta["endosatario"].(map[string]interface{}); ok {
			end.Endosatario = strings.TrimSpace(strVal(en["nombre"]) + " " + strVal(en["apellido"]))
			end.NIF = strVal(en["nif"])
		}
		toPub := strVal(meta["to"])
		if end.Endosatario == "" && toPub != "" {
			if u := h.resolveUser(toPub); u != nil {
				end.Endosatario = strings.TrimSpace(u.Nombre + " " + u.Apellido)
				end.NIF = u.NIF
			} else {
				end.Endosatario = "clave " + shortIDStr(toPub)
			}
		}
		endosos = append(endosos, end)
	}
	return endosos, cesiones, firmantePub
}

// shortIDStr abbreviates a long identifier/pubkey for display.
func shortIDStr(s string) string {
	if len(s) <= 16 {
		return s
	}
	return s[:8] + "…" + s[len(s)-6:]
}

func strVal(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// OwnsAsset reports whether the principal owns (or appears in the history of)
// the asset. Exported so other endpoints (e.g. alert filtering) can reuse the
// same ownership rule as the consulta views.
func (h *ConsultaHandler) OwnsAsset(id string, p *auth.Principal) bool {
	return h.assetOwnedBy(id, p)
}

// figuraComoPropietario indica si el usuario consta como dueño del pagaré. Es
// el primer criterio de "es mío", y el barato: una sola consulta.
func (h *ConsultaHandler) figuraComoPropietario(id string, p *auth.Principal) bool {
	if p == nil {
		return false
	}
	body, status, err := h.client.GetAssetOwners(id)
	if err != nil || status != 200 {
		return false
	}
	var o map[string]interface{}
	if json.Unmarshal(body, &o) != nil {
		return false
	}
	owners, _ := o["owners"].([]interface{})
	for _, ow := range owners {
		if m, ok := ow.(map[string]interface{}); ok {
			if pub, ok := m["pub"].(string); ok && p.HasPubKey(pub) {
				return true
			}
		}
	}
	return false
}

func (h *ConsultaHandler) assetOwnedBy(id string, p *auth.Principal) bool {
	if p == nil {
		return false
	}
	if h.figuraComoPropietario(id, p) {
		return true
	}

	// Also consider the asset as "mine" if my pub appears anywhere in its history
	// (creation from, endoso to/from, etc.) - this makes "cualquiera que tuviera
	// mi identidad" work better.
	return apareceEnHistorial(h.historialDe(id), p)
}

// apareceEnHistorial indica si la clave del usuario sale en algún punto del
// histórico: creación, endoso, cesión… Es el segundo criterio de "es mío",
// después de figurar como propietario.
func apareceEnHistorial(history []interface{}, p *auth.Principal) bool {
	if p == nil {
		return false
	}
	for _, entry := range history {
		e, _ := entry.(map[string]interface{})
		if e == nil {
			continue
		}
		meta, _ := e["metadata"].(map[string]interface{})
		if meta == nil {
			continue
		}
		// Check common places where pub may appear
		for _, key := range []string{"from", "to", "pub", "identidad_blockchain", "firma_digital_pagare", "firma_digital_beneficiario", "firma_digital_endoso"} {
			if v, ok := meta[key].(string); ok && p.HasPubKey(v) {
				return true
			}
			if m, ok := meta[key].(map[string]interface{}); ok {
				if pub, ok := m["pub"].(string); ok && p.HasPubKey(pub) {
					return true
				}
			}
		}
	}
	return false
}
