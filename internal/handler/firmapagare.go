package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"pagare/internal/auth"
	"pagare/internal/firma"
	"pagare/internal/models"
	"pagare/internal/pdf"
)

// Firmantes resuelve quién firma y cómo avisarle. El portal de firma exige
// nombre, NIF, correo y móvil del firmante, y los tres últimos sólo los tiene
// la cuenta. Satisfecho por *auth.Store.
type Firmantes interface {
	GetByID(id string) (*auth.User, error)
}

// FirmaPDF es lo que el handler necesita de la firma cualificada. Es una
// interfaz y no el tipo concreto para poder probar el ir y venir de una firma
// —que es la parte con más aristas— sin hablar con el portal.
//
// Satisfecha por *firma.Servicio, incluido el nil que significa "desactivada".
type FirmaPDF interface {
	Activa() bool
	Iniciar(ctx context.Context, p firma.Peticion) (*firma.Envio, error)
	Consultar(ctx context.Context, referencia string) (*firma.Situacion, error)
	Recoger(ctx context.Context, guid, hashOriginal string) (*firma.Firmado, error)
}

// SetFirma conecta la firma cualificada del PDF: el servicio que habla con
// Logalty, el almacén de las firmas pedidas y el resolutor de firmantes.
//
// Sin esto la firma queda desactivada y las operaciones se completan en el
// acto, como antes de que existiera.
func (h *PagareHandler) SetFirma(s FirmaPDF, reg *firma.Registros, f Firmantes) {
	h.firma, h.firmas, h.firmantes = s, reg, f
}

// FirmaActiva indica si las operaciones exigen firmar el PDF.
func (h *PagareHandler) FirmaActiva() bool {
	return h.firma != nil && h.firma.Activa() && h.firmas != nil && h.firmantes != nil
}

// pendienteEmision es la entrega al beneficiario que espera a la firma.
//
// Se guarda la clave ya resuelta y no el pagaré entero: cuando la firma llegue,
// completar sólo necesita saber a quién entregar y con qué identidad firmar la
// transferencia.
type pendienteEmision struct {
	A           string `json:"a,omitempty"`
	PubFirmante string `json:"pub_firmante"`
}

// pendienteEndoso es el endoso que espera a la firma.
type pendienteEndoso struct {
	A           string                 `json:"a"`
	PubFirmante string                 `json:"pub_firmante"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// referenciaDe es el identificador con el que se sigue el envío en el portal.
// Lleva el pagaré y la operación porque es lo que lo hace único y legible en el
// listado del portal.
func referenciaDe(op firma.Operacion, assetID string, intento int) string {
	if intento <= 1 {
		return fmt.Sprintf("pagare-%s-%s", assetID, op)
	}
	return fmt.Sprintf("pagare-%s-%s-%d", assetID, op, intento)
}

// pedirFirma genera el PDF de la operación y manda al firmante a firmarlo.
//
// El PDF es el definitivo, con el id del pagaré y su QR de verificación: se
// firma exactamente el documento que cualquiera puede después descargar y
// cotejar.
func (h *PagareHandler) pedirFirma(r *http.Request, op firma.Operacion, assetID string,
	entrada pdf.Input, userID string, pendiente any) (*firma.Registro, error) {

	if !h.FirmaActiva() {
		return nil, firma.ErrDesactivada
	}

	u, err := h.firmantes.GetByID(userID)
	if err != nil {
		return nil, fmt.Errorf("no se pudo cargar al firmante: %w", err)
	}

	entrada.AssetID = assetID
	entrada.VerifyURL = urlDeVerificacion(r, assetID)
	documento, err := pdf.Generate(entrada)
	if err != nil {
		return nil, fmt.Errorf("no se pudo generar el PDF a firmar: %w", err)
	}

	espera, err := json.Marshal(pendiente)
	if err != nil {
		return nil, fmt.Errorf("no se pudo anotar la operación en espera: %w", err)
	}

	// Un reintento sobre el mismo pagaré necesita otra referencia, porque la
	// anterior ya identifica un envío en el portal.
	intento := 1
	if anteriores, err := h.firmas.Ultima(assetID); err == nil && anteriores != nil {
		intento = 2
		for i := 2; i < 20; i++ {
			if _, err := h.firmas.PorReferencia(referenciaDe(op, assetID, i)); errors.Is(err, firma.ErrNoEncontrada) {
				intento = i
				break
			}
		}
	}
	referencia := referenciaDe(op, assetID, intento)

	envio, err := h.firma.Iniciar(r.Context(), firma.Peticion{
		Referencia: referencia,
		Fichero:    fmt.Sprintf("pagare-%s.pdf", assetID),
		Asunto:     asuntoDe(op),
		PDF:        documento,
		Firmante: firma.Firmante{
			Nombre:    u.Nombre,
			Apellidos: u.Apellido,
			NIF:       u.NIF,
			Email:     u.Email,
			Movil:     u.Telefono,
		},
	})
	if err != nil {
		return nil, err
	}

	reg := &firma.Registro{
		AssetID:      assetID,
		Operacion:    op,
		UserID:       userID,
		Referencia:   envio.Referencia,
		GUID:         envio.GUID,
		HashOriginal: envio.HashOriginal,
		Pendiente:    espera,
	}
	if err := h.firmas.Crear(reg); err != nil {
		// El envío ya salió, así que el firmante recibirá el aviso: decirlo es
		// mejor que dejarlo en silencio y que firme algo que no vamos a recoger.
		return nil, fmt.Errorf("la firma se pidió (%s) pero no se pudo registrar: %w",
			envio.Referencia, err)
	}
	return reg, nil
}

func asuntoDe(op firma.Operacion) string {
	if op == firma.Endoso {
		return "Firma del endoso de un pagaré"
	}
	return "Firma de la emisión de un pagaré"
}

// urlDeVerificacion es el enlace público que lleva el QR del PDF.
func urlDeVerificacion(r *http.Request, assetID string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/pagares/verificar?network=test&id=%s", scheme, r.Host, assetID)
}

// Completar mira si una firma pendiente ya se ha firmado y, si es así, recoge
// el PDF y ejecuta la operación que estaba esperando.
//
// Devuelve el registro tal como queda. Un error de red no cambia nada: la firma
// sigue pendiente y se volverá a mirar.
func (h *PagareHandler) Completar(ctx context.Context, reg *firma.Registro) (*firma.Registro, error) {
	if !h.FirmaActiva() || !reg.EnCurso() {
		return reg, nil
	}

	sit, err := h.firma.Consultar(ctx, reg.Referencia)
	if err != nil {
		return reg, err
	}
	if sit.GUID != "" && sit.GUID != reg.GUID {
		reg.GUID = sit.GUID
		_ = h.firmas.AnotarGUID(reg.ID, sit.GUID)
	}
	if !sit.Terminado {
		return reg, nil
	}
	if !sit.Firmado {
		reg.Estado, reg.Motivo = firma.Fallida, sit.Descripcion
		return reg, h.firmas.Fallar(reg.ID, sit.Descripcion)
	}
	if reg.GUID == "" {
		// Terminado y firmado pero sin GUID no debería pasar; sin él no hay
		// documento que descargar, así que se deja pendiente y se reintenta.
		return reg, nil
	}

	firmado, err := h.firma.Recoger(ctx, reg.GUID, reg.HashOriginal)
	if err != nil {
		// Que el portal haya firmado otro documento, o que no superara sus
		// propias comprobaciones, es un resultado y cierra la firma. Un fallo
		// de transporte, no.
		if esRechazoDefinitivo(err) {
			reg.Estado, reg.Motivo = firma.Fallida, err.Error()
			return reg, h.firmas.Fallar(reg.ID, err.Error())
		}
		return reg, err
	}
	if err := h.firmas.Resolver(reg, firmado); err != nil {
		return reg, err
	}

	// La firma ya está guardada; lo que queda es la operación que esperaba. Si
	// falla, la firma no se pierde: se puede reintentar sin volver a firmar.
	return reg, h.ejecutarPendiente(reg)
}

// esRechazoDefinitivo distingue un resultado del portal de una avería de red.
// Sólo el primero cierra la firma, porque reintentar no lo va a cambiar.
func esRechazoDefinitivo(err error) bool {
	return strings.Contains(err.Error(), "firmó otro documento") ||
		strings.Contains(err.Error(), "no devolvió PDF firmado")
}

// ejecutarPendiente hace lo que la operación dejó a medias esperando la firma.
func (h *PagareHandler) ejecutarPendiente(reg *firma.Registro) error {
	switch reg.Operacion {
	case firma.Emision:
		var p pendienteEmision
		if err := json.Unmarshal(reg.Pendiente, &p); err != nil {
			return fmt.Errorf("la entrega en espera no se pudo leer: %w", err)
		}
		if p.A == "" {
			// El beneficiario no tenía identidad al emitir. El pagaré queda
			// firmado y pendiente de entrega, que es un estado legítimo.
			return nil
		}
		desde, err := h.identidadDe(reg.UserID, p.PubFirmante)
		if err != nil {
			return err
		}
		if e := h.entregar(reg.AssetID, nil, p.A, desde); !e.Entregado {
			return errors.New(e.Msg)
		}
		return nil

	case firma.Endoso:
		var p pendienteEndoso
		if err := json.Unmarshal(reg.Pendiente, &p); err != nil {
			return fmt.Errorf("el endoso en espera no se pudo leer: %w", err)
		}
		desde, err := h.identidadDe(reg.UserID, p.PubFirmante)
		if err != nil {
			return err
		}
		cuerpo := map[string]interface{}{
			"id":       reg.AssetID,
			"to":       p.A,
			"metadata": p.Metadata,
			"from":     map[string]string{"pub": desde.Pub, "pvt": desde.Pvt},
		}
		_, status, err := h.client.UpdateAsset(cuerpo)
		if err != nil {
			return fmt.Errorf("el endoso está firmado pero la red no lo aceptó: %w", err)
		}
		if status != 200 {
			return fmt.Errorf("el endoso está firmado pero la red lo rechazó con %d", status)
		}
		return nil

	default:
		return fmt.Errorf("operación en espera desconocida: %q", reg.Operacion)
	}
}

// identidadDe rearma la identidad de firma del usuario a partir de su clave
// pública, abriendo la privada sellada. Hace falta porque la operación se
// completa fuera de la petición que la pidió, sin sesión de la que tirar.
func (h *PagareHandler) identidadDe(userID, pub string) (*models.IdentidadBC, error) {
	if pub == "" {
		return nil, errors.New("no se anotó la clave del firmante")
	}
	pvt, err := h.keys.GetPrivateKey(userID, pub)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir la clave del firmante: %w", err)
	}
	return &models.IdentidadBC{Pub: pub, Pvt: pvt}, nil
}

// CompletarEnEspera revisa todas las firmas pendientes. Devuelve cuántas ha
// mirado y cuántas han dejado de estar pendientes.
func (h *PagareHandler) CompletarEnEspera(ctx context.Context) (revisadas, resueltas int, err error) {
	if !h.FirmaActiva() {
		return 0, 0, firma.ErrDesactivada
	}
	pendientes, err := h.firmas.EnEspera()
	if err != nil {
		return 0, 0, err
	}
	for _, reg := range pendientes {
		revisadas++
		if _, err := h.Completar(ctx, reg); err != nil {
			fmt.Printf("[firma] %s (%s): %v\n", reg.AssetID, reg.Operacion, err)
		}
		if !reg.EnCurso() {
			resueltas++
		}
	}
	return revisadas, resueltas, nil
}

// vistaFirma es lo que se cuenta de una firma por la API. No lleva la ruta del
// PDF en disco: para eso está la descarga.
func vistaFirma(reg *firma.Registro) map[string]interface{} {
	if reg == nil {
		return map[string]interface{}{"estado": "", "msg": "Sin firma pedida"}
	}
	v := map[string]interface{}{
		"estado":        string(reg.Estado),
		"operacion":     string(reg.Operacion),
		"referencia":    reg.Referencia,
		"hash_original": reg.HashOriginal,
		"creada_at":     reg.CreadaAt,
	}
	if reg.HashFirmado != "" {
		v["hash_firmado"] = reg.HashFirmado
	}
	if reg.Motivo != "" {
		v["motivo"] = reg.Motivo
	}
	if reg.ResueltaAt != nil {
		v["resuelta_at"] = reg.ResueltaAt
	}
	switch reg.Estado {
	case firma.Pendiente:
		v["msg"] = "Pendiente de firma: el firmante tiene el enlace en su correo y su móvil"
	case firma.Firmada:
		v["msg"] = "Firmado con firma cualificada y sello de tiempo"
	case firma.Fallida:
		v["msg"] = "La firma no se completó"
	}
	return v
}

// EstadoFirma informa de cómo va la firma de un pagaré, preguntando al portal
// si hace falta y completando la operación en espera si ya se firmó.
func (h *PagareHandler) EstadoFirma(w http.ResponseWriter, r *http.Request) {
	if auth.GetPrincipal(r) == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}
	if !h.FirmaActiva() {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "activa": false, "msg": "la firma del PDF no está configurada"})
		return
	}

	reg, err := h.firmas.Ultima(id)
	if errors.Is(err, firma.ErrNoEncontrada) {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "activa": true, "firma": vistaFirma(nil)})
		return
	}
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}

	reg, errCompletar := h.Completar(r.Context(), reg)
	res := map[string]interface{}{"ok": true, "activa": true, "firma": vistaFirma(reg)}
	if errCompletar != nil {
		res["aviso"] = errCompletar.Error()
	}
	WriteJSON(w, http.StatusOK, res)
}

// DescargarPDFFirmado sirve el PDF con la firma cualificada, que es el
// documento con valor probatorio: lleva la firma PAdES del prestador y su sello
// de tiempo, no sólo la firma ed25519 de la red.
func (h *PagareHandler) DescargarPDFFirmado(w http.ResponseWriter, r *http.Request) {
	if auth.GetPrincipal(r) == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" || !h.FirmaActiva() {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}

	reg, err := h.firmas.Ultima(id)
	if err != nil || reg.Estado != firma.Firmada {
		WriteJSON(w, http.StatusNotFound, map[string]interface{}{
			"ok": false, "msg": "este pagaré no tiene PDF firmado"})
		return
	}
	documento, err := h.firmas.PDFFirmado(reg)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"ok": false, "msg": "el PDF firmado no se pudo leer"})
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="pagare-%s-firmado.pdf"`, id))
	w.Write(documento)
}

// endosoParaPDF traduce el endoso que se está haciendo a la fila que el PDF
// pinta en el reverso. Es el endoso que se firma, así que va en el documento
// aunque todavía no esté en la cadena.
func endosoParaPDF(e *models.Endoso, endosantePub string) pdf.Endoso {
	fila := pdf.Endoso{
		Tipo:         e.Tipo,
		Clausula:     e.Clausula,
		EndosantePub: endosantePub,
	}
	if len(e.Fecha) >= 10 {
		fila.Fecha = e.Fecha[:10]
	}
	if e.Endosatario != nil {
		fila.Endosatario = strings.TrimSpace(e.Endosatario.Nombre + " " + e.Endosatario.Apellido)
		fila.NIF = e.Endosatario.NIF
	}
	return fila
}
