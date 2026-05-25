# Plan: Aplicación de Pagaré Electrónico en BlockchainFUE

## Fase 1 - Contrato API (Swagger YAML)

- Definir OpenAPI 3.0 con:
  - Endpoints de BlockchainFUE (crear asset, transferir, quemar, consultar, histórico, identidades, firma/verificación)
  - Modelo `PagareElectronico` basado en `pagare.json`
  - Endpoints de alto nivel del dominio: emitir, endosar, pagar, consultar pagaré
- **Entregable:** `openapi.yaml` (ya generado)

## Fase 2 - Backend (servicio intermediario)

- API REST propia que envuelve BlockchainFUE
- Mapeo pagaré → asset:
  - `data` = campos inmutables del pagaré (denominación, importe, vencimiento, firmante, beneficiario...)
  - `metadata` = cambios de estado (endosos, pagos, anulaciones)
- Gestión de identidades (keypair/DID) para firmante y beneficiario
- Firmas digitales del contenido del pagaré antes de grabar
- Cifrado de datos sensibles (NIF, dirección) con las funciones de BlockchainFUE

## Fase 3 - Ciclo de vida del pagaré en blockchain

| Estado | Acción BlockchainFUE | Metadata |
|--------|----------------------|----------|
| **Emisión** | `POST /api/asset` (crear asset con type `pagare_electronico`) | `action: CREATE`, datos del pagaré firmados |
| **Endoso** | `PUT /api/asset` (transferir a nuevo beneficiario) | `action: ENDOSO`, nuevo beneficiario, tipo de endoso |
| **Pago** | `DELETE /api/asset` (quemar = pagado/inutilizado) | `action: PAGO`, fecha y referencia |
| **Anulación** | `DELETE /api/asset` (quemar por acuerdo) | `action: ANULACION`, motivo |
| **Prescripción** | `DELETE /api/asset` (quemar tras 3 años, art. 48 LCCH) | `action: PRESCRIPCION` |
| **Consulta** | `GET /api/asset` + `GET /api/asset/history` | Cadena completa de custodia |

## Fase 4 - Seguridad

- DID (Sovrin) para identidad digital de las partes
- Firma criptográfica del pagaré completo antes de grabar
- Cifrado de datos sensibles (NIF, dirección) con las funciones de BlockchainFUE
- Verificación pública de firmas

## Fase 5 - Frontend

- Formulario de emisión de pagarés
- Dashboard de pagarés emitidos/recibidos
- Visor de histórico (cadena de endosos)
- Verificación pública de un pagaré (endpoint público)

---

## Validación legal LCCH (Ley Cambiaria y del Cheque)

### Art. 94 - Requisitos formales del pagaré

Campos obligatorios validados automáticamente:

- La palabra "PAGARÉ" en el título (`denominacion`)
- Promesa pura y simple de pagar una cantidad determinada (`promesa_pago = true`)
- Nombre del beneficiario o persona a cuya orden se paga
- Fecha de vencimiento o "a la vista"
- Lugar de pago
- Nombre, firma y lugar de emisión del firmante

### Art. 39 - Pagaré a la vista

- Plazo máximo de 1 año desde la emisión
- El librador puede establecer un plazo mayor o menor

### Arts. 14-24 - Endoso (aplicables al pagaré por el art. 96)

El endoso del pagaré se rige por las normas de la letra de cambio (arts. 14-24),
por remisión expresa del art. 96 LCCH.

Tipos de endoso soportados:

- **En propiedad** (por defecto): transmite todos los derechos del pagaré (art. 17)
- **En procuración** / comisión de cobranza: solo autoriza el cobro, no transfiere titularidad ("valor al cobro", "por poder") (art. 21)
- **En blanco**: sin designar endosatario; el endoso al portador equivale al endoso en blanco (arts. 15-17)
- **En garantía / en prenda**: el pagaré se entrega en prenda como garantía ("valor en garantía", "valor en prenda"); no transmite la propiedad (art. 22)

> Nota: el endoso posterior al vencimiento o tras el protesto produce los efectos de una cesión ordinaria (art. 23); no es un tipo elegible sino una consecuencia temporal.

Cláusulas:

- **Sin responsabilidad** / "sin garantía": "sin mi responsabilidad", el endosante se exonera de garantizar el pago (art. 18)
- **Prohibición de nuevo endoso** ("no a la orden" del endosante): el endosante no responde ante endosatarios posteriores (art. 18)
- **Sin gastos / sin protesto**: dispensa al tenedor de levantar protesto para ejercer la acción de regreso (art. 56)

La cláusula **"no a la orden" puesta por el librador** (art. 14) se fija en la **emisión** del pagaré (no en el endoso): priva al título de su condición de endosable, de modo que solo puede transmitirse por cesión ordinaria.

### Arts. 35-37 - Aval (aplicables al pagaré por el art. 96)

- Aval total o parcial
- Datos del avalista (persona/DID)

### Art. 48 - Prescripción

- Plazo de prescripción: 3 años
- Estado final `PRESCRIPCION`

---

## Endoso múltiple

### Flujo

1. **Emisión**: Firmante A crea el pagaré a favor de beneficiario B
2. **Endoso 1**: B endosa en propiedad a C → transferencia en blockchain
3. **Endoso 2**: C endosa en procuración a D → solo autorización de cobro
4. **Endoso N**: ... la cadena puede continuar indefinidamente
5. **Pago**: El titular legítimo cobra, el pagaré se quema

### Registro en blockchain

Cada endoso genera:

- Operación `TRANSFER` (o `UPDATE` si es procuración) en el asset
- Metadata con:
  - `action: ENDOSO`
  - `tipo_endoso`: en_propiedad / en_procuracion / en_blanco
  - `endosante`: clave pública del endosante
  - `endosatario`: clave pública del endosatario
  - `firma_digital_endoso`: firma DID del endosante sobre el endoso
  - `clausula`: sin_clausula / sin_responsabilidad / no_a_la_orden

### Consulta

- `GET /asset/history` devuelve la cadena completa de endosos con valor probatorio
- Cada entrada incluye fecha, identidad y firma digital
- El modelo `CadenaEndosos` agrupa toda la cadena con el titular actual

---

## Stack tecnológico

### Backend

| Capa | Tecnología | Por qué |
|------|-----------|---------|
| Lenguaje | Go | Binario estático, alto rendimiento, bajo consumo |
| HTTP framework | Chi | Ligero, idiomático, buen middleware |
| API client | `net/http` estándar | Cliente REST para BlockchainFUE sin dependencias extra |
| Validación | `go-playground/validator` + custom | Tags en structs + lógica LCCH propia |
| JSON Schema | Validación manual desde `pagare.json` | Validar pagarés contra el esquema |

### Frontend (SSR con Go)

| Capa | Tecnología | Por qué |
|------|-----------|---------|
| Templates | Templ | Type-safe, compilado, SSR nativo en Go |
| Interactividad | HTMX | SPA-like sin JS framework, ideal para forms y dashboards |
| CSS | Tailwind CSS | Utility-first, sin build pipeline complejo |
| Iconos | Lucide | SVG inline en Templ |

### Infraestructura

| Capa | Tecnología | Por qué |
|------|-----------|---------|
| Runtime | Binario Go compilado estáticamente | Deploy: un solo binario en el VPS |
| Reverse proxy | Caddy | TLS automático |
| Cache (opcional) | SQLite o Redis | Índice off-chain de pagarés si las búsquedas son lentas |
| Gestión proceso | Systemd | Service unit en el VPS |
| Blockchain | BlockchainFUE | MAIN producción, TEST desarrollo |

### Ventajas del stack

- Un solo binario (Go compila templates Templ en el binario)
- Sin Node.js en producción (el frontend es SSR + HTMX)
- Bajo consumo de RAM y CPU en el VPS
- Muy rápido para el tráfico esperado de pagarés
- Type-safe de extremo a extremo (Templ genera Go)

---

## Estructura de archivos

```
pagare/
├── cmd/
│   └── server/
│       └── main.go              # Entry point
├── internal/
│   ├── config/                  # Config (env vars, API keys)
│   ├── models/                  # Pagare, Persona, Endoso, etc.
│   ├── validator/               # Validaciones LCCH
│   ├── bcfclient/               # Cliente BlockchainFUE
│   ├── crypto/                  # Servicio criptográfico (DID)
│   ├── service/                 # Lógica de negocio
│   ├── handler/                 # HTTP handlers (Chi)
│   └── templates/               # Templ templates
├── web/
│   ├── static/                  # CSS (Tailwind), JS (HTMX), assets
│   └── public/                  # Archivos públicos
├── pagare.json                  # Esquema JSON original del pagaré
├── openapi.yaml                 # Contrato API Swagger/OpenAPI 3.0
├── plan.md                      # Este documento
├── go.mod
├── go.sum
├── Makefile
└── Dockerfile                   # Opcional para containerizar
