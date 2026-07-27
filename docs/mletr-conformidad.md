# Matriz de conformidad MLETR / eIDAS2 de la plataforma

Estado de la implementación frente a los requisitos funcionales del **método
fiable** de la Ley Modelo UNCITRAL sobre Documentos Transmisibles Electrónicos
(MLETR, 2017) y al régimen del **libro mayor electrónico** del Reglamento (UE)
2024/1183 (eIDAS2).

Marco doctrinal de referencia: [`pagare-tokenizado.md`](pagare-tokenizado.md)
(Pastor Sempere, 2026). Menciones formales del título:
[`pagare-forma-legal.md`](pagare-forma-legal.md).

> Documento técnico interno del proyecto. Describe lo que la infraestructura
> cubre **hoy** y lo que no; no es asesoramiento jurídico ni declaración de
> conformidad frente a un organismo de supervisión.

## 1. Los cinco requisitos del método fiable

La formulación abstracta de los cinco requisitos procede del análisis comparado
de las transposiciones nacionales (§4.5 del documento doctrinal), y se
corresponde casi literalmente con las cinco condiciones del artículo 16 de la
*Loi n.º 2024-537* francesa y con los cuatro requisitos del `reliable system` de
la sección 2(2) de la *Electronic Trade Documents Act 2023* británica.

| # | Requisito | Implementación | Estado |
|---|-----------|----------------|--------|
| i | **Unicidad** del registro | Un asset por pagaré en BlockchainFUE; el consenso de la red impide la doble transmisión. Emisión con `CreateAsset`, extinción con `BurnAsset` (`internal/bcfclient/client.go`) | **Cubierto** (unicidad técnica, no presunción legal — §3) |
| ii | **Identificación** del titular del control | Par de claves ed25519 por usuario (`internal/crypto`), asociación usuario↔pubkey en `internal/auth`, consulta de titularidad vía `GetAssetOwners` | **Parcial** — identidad de plataforma, no eIDAS (§4) |
| iii | **Exclusividad** del control | Propiedad del asset en la red; solo el poseedor de la clave privada puede transferirlo. La red rechaza la operación si `from` no es el titular | **Parcial** — control ejercido por tercero (§5) y no transferido en la emisión (§6) |
| iv | **Trazabilidad** de la cadena de portadores | `GetAssetHistory` → `parseHistory` (`internal/handler/consulta.go`) → cadena de endosos en el reverso del PDF (`internal/pdf/pagare.go`). Orden cronológico garantizado por la red | **Cubierto** |
| v | **Integridad** de la información incorporada | Inmutabilidad del registro en la red | **Insuficiente** — falta firma separable del contenido (§7) |

## 2. Régimen aplicable y exclusiones

La infraestructura opera como **libro mayor electrónico no cualificado** a
efectos del artículo 3.52 eIDAS2, bajo el régimen del artículo 19 bis y del
Reglamento de Ejecución (UE) 2025/2160, con la norma técnica ETSI EN 319 401
como referencia.

Delimitación negativa del objeto (§2 del documento doctrinal):

- **No es** un medio de pago en sentido PSD2 (Directiva (UE) 2015/2366), ni del
  futuro paquete PSD3/PSR.
- **No es** un criptoactivo sujeto a MiCA (Reglamento (UE) 2023/1114): el token
  no se ofrece al público ni se admite a negociación, opera como registro de
  control de un derecho cambiario subyacente.
- **No es** un valor negociable, por lo que no le alcanza el carril del artículo
  517 LEC abierto por la Ley 6/2023 para valores representados mediante DLT.
- En consecuencia, la circulación del título queda **fuera** del perímetro de la
  «regla del viaje» (Reglamento (UE) 2023/1113); esa obligación se localiza, en
  su caso, en la liquidación final mediante operación de pago.

## 3. Unicidad técnica frente a presunción legal

La unicidad que el consenso garantiza es **fáctica**. La presunción legal de
unicidad, exactitud del orden cronológico e integridad se reserva, conforme al
artículo 45 duodecies, apartado 2, eIDAS2, a los registros contenidos en un
libro mayor electrónico **cualificado**, lo que exigiría la cualificación del
prestador conforme al artículo 45 terdecies.

La plataforma no aspira a la cualificación en esta fase. Consecuencia práctica:
en un eventual litigio, la unicidad del registro habrá de **probarse**, no se
presume.

Añadido a lo anterior, y en el plano procesal: la doctrina del Tribunal Supremo
exige, para el acceso al juicio cambiario, la aportación del documento original
(STS, Sala Primera, núm. 94/2014, de 5 de marzo). **El PDF que la plataforma
genera no es título ejecutivo**; es una representación legible del registro
electrónico. Salvar esto requiere reforma de la LCCH y de la LEC.

## 4. Identidad: lo que la firma acredita y lo que no

Las claves son ed25519 provisionadas por la propia red (`crypto.GenerateKeypair`).
**No constituyen firma electrónica cualificada** en el sentido del artículo 25
eIDAS: no hay certificado cualificado, ni prestador cualificado, ni dispositivo
cualificado de creación de firma.

Distinción del §3.4 del documento doctrinal, que la implementación debe respetar:

- **Identidad del firmante** — quién firma. Hoy: credenciales de plataforma.
  Horizonte: Cartera Europea de Identidad Digital y firma cualificada eIDAS2.
- **Poder de representación** — si quien firma puede obligar a la sociedad. Hoy:
  **no se acredita**; el modelo `Firmante` solo admite persona física. Horizonte:
  credencial verificable de fuente registral vía European Business Wallet
  (propuesta COM(2025) 838, aún en tramitación) y poder de representación digital
  de la Directiva (UE) 2025/25, no aplicable hasta el 1 de agosto de 2028.

Mientras tanto, la representación societaria debe acreditarse por medios
convencionales (nota simple o copia autorizada del poder), fuera de la
plataforma.

## 5. Custodia de claves: el control ejercido por tercero

La plataforma **custodia las claves privadas** de los usuarios, selladas con
AES-GCM en `internal/keyvault` y recuperadas por `resolveFrom`
(`internal/handler/pagare.go`) para firmar en nombre del usuario autenticado.
Existe una vía alternativa no custodial: el cliente puede aportar su clave
privada en `from.pvt` y la plataforma la usa sin almacenarla.

Esto es admisible bajo el modelo MLETR, cuyo artículo 11 admite el control
ejercido «por sí o por un tercero», como recoge expresamente el artículo 15.II de
la ley francesa (*«de son contrôle exclusif [...] par lui ou par un tiers»*).
Pero conviene ser explícito sobre su alcance: en la configuración por defecto,
**la exclusividad del control es una garantía organizativa de la plataforma, no
una propiedad criptográfica en manos del tenedor**. Quien opera la plataforma
puede, técnicamente, firmar por cualquier usuario.

Es la objeción que el §3.3 del documento doctrinal dirige a las soluciones de
tercero de confianza. Se documenta aquí, no se oculta: el modo no custodial
existe, y la migración a claves en cartera de identidad es el horizonte natural.

## 6. La entrega: control no transferido en la emisión

**Divergencia conocida.** En la emisión, el asset se crea con `from` = firmante
y sin destinatario, de modo que el emisor **conserva el control** del registro.
El beneficiario no lo adquiere hasta que se produce una transferencia posterior.

En el título en papel, la posesión pasa al tenedor por la **entrega**. Sin un
equivalente electrónico de la entrega, el requisito (iii) del método fiable
—control exclusivo del portador— no queda satisfecho en el momento de la emisión.

Pendiente de resolver: transferencia del control al beneficiario como parte
atómica de la operación de emisión.

## 7. Integridad: firma del contenido

**Divergencia conocida.** El contenido del pagaré (las menciones del artículo 94
LCCH) **no se firma de forma separable**. La integridad descansa hoy únicamente
en la inmutabilidad del asset en la red.

Los medios existen y están sin cablear: `crypto.SignPagareContent` y
`crypto.VerifyPagareSignature` (`internal/crypto/service.go`) no se invocan desde
ningún punto del flujo, y el formulario de emisión no envía
`firma_digital_pagare`. La consecuencia es que un tercero no puede verificar la
integridad del contenido con independencia de la red: debe confiar en ella.

Pendiente de resolver: firma del JSON canónico del pagaré en la emisión y
verificación pública en `/pagares/verificar`.

## 8. Validación sustantiva LCCH

Al margen de los cinco requisitos del método fiable —que operan en el plano de la
infraestructura—, la plataforma valida las menciones sustantivas del título y
mapea cada error a su artículo (`internal/validator/lcch.go`):

| Materia | Artículos LCCH | Cobertura |
|---|---|---|
| Menciones esenciales del pagaré | art. 94 | Las siete, con mensajes por artículo |
| Suplencias | art. 95 | Parcial (vencimiento a la vista) |
| Importe en cifra y letra | art. 7 | Conversión número→letra en el PDF |
| Vencimiento a la vista, plazo de un año | art. 39 | Aviso al emitir |
| Cláusula «no a la orden» | art. 14 | Modelo y PDF; **no bloquea el endoso** |
| Endoso: propiedad, blanco, procuración, garantía | arts. 15, 17, 21, 22 | Los cuatro tipos |
| Cláusulas del endoso | arts. 18, 56 | Sin responsabilidad, prohibición de nuevo endoso, sin gastos |
| Aval total y parcial | arts. 35-37 | Con avalado y tope del principal |
| Prescripción | art. 88 | Comprobación periódica (`internal/scheduler`) |

Nótese que esta cobertura excede el ámbito de la fase inicial descrita en el
documento doctrinal (§8.3), que se limita a pagarés no a la orden: la
implementación del endoso está hecha, y es el plano institucional —no el
técnico— el que sigue sin resolver la eficacia cambiaria de su circulación
electrónica.

## 9. Resumen de lo pendiente

| Plano | Pendiente | Depende de |
|---|---|---|
| Técnico | Entrega del control al beneficiario en la emisión (§6) | Nosotros |
| Técnico | Firma separable del contenido y su verificación (§7) | Nosotros |
| Técnico | Bloqueo del endoso en pagarés «no a la orden» (§8) | Nosotros |
| Modelo | Persona jurídica y poder de representación (§4) | Nosotros, con horizonte EBW |
| Institucional | Presunción legal de unicidad (§3) | Cualificación del prestador |
| Institucional | Firma cualificada eIDAS y cartera de identidad (§4) | Despliegue EUDI Wallet |
| Institucional | Representación societaria verificable (§4) | COM(2025) 838; Directiva 2025/25 (2028) |
| Legislativo | Eficacia cambiaria plena y acceso al juicio cambiario (§3) | Reforma LCCH + LEC |
