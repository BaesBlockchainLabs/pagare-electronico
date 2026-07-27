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

La interfaz de la plataforma está descrita en [`openapi.yaml`](../openapi.yaml) y
la de la red que consume en [`openapi-bcf.yaml`](../openapi-bcf.yaml); el propio
servicio las sirve en `/docs`.

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
| iii | **Exclusividad** del control | Propiedad del asset en la red, transferida al beneficiario en la emisión (§6); solo el poseedor de la clave privada puede transferirla, y la red rechaza la operación si `from` no es el titular | **Parcial** — resuelto salvo por la custodia de claves (§5) |
| iv | **Trazabilidad** de la cadena de portadores | `GetAssetHistory` → `parseHistory` (`internal/handler/consulta.go`) → cadena de endosos en el reverso del PDF (`internal/pdf/pagare.go`). Orden cronológico garantizado por la red | **Cubierto** |
| v | **Integridad** de la información incorporada | Inmutabilidad del registro, más firma del emisor sobre la forma canónica del contenido, verificable públicamente (§7) | **Cubierto** |

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

Esto **se advierte al usuario**, en el anverso del PDF y en la pantalla de
verificación, y no como letra pequeña: el sistema decía antes que el registro era
«fehaciente e inalterable», y fehaciente significa precisamente que prueba por sí
mismo, que es lo que un libro mayor no cualificado no hace. La palabra se ha
retirado de todas partes.

Añadido a lo anterior, y en el plano procesal: la doctrina del Tribunal Supremo
exige, para el acceso al juicio cambiario, la aportación del documento original
(STS, Sala Primera, núm. 94/2014, de 5 de marzo). **El PDF que la plataforma
genera no es título ejecutivo**; es una representación legible del registro
electrónico. Salvar esto requiere reforma de la LCCH y de la LEC. También consta
en el propio documento y en la pantalla de verificación, para que nadie lo
descubra al intentar ejecutarlo.

## 4. Identidad: lo que la firma acredita y lo que no

Las claves son ed25519 provisionadas por la propia red (`crypto.GenerateKeypair`).
**No constituyen firma electrónica cualificada** en el sentido del artículo 25
eIDAS: no hay certificado cualificado, ni prestador cualificado, ni dispositivo
cualificado de creación de firma.

Distinción del §3.4 del documento doctrinal, que la implementación debe respetar:

- **Identidad del firmante** — quién firma. Hoy: credenciales de plataforma.
  Horizonte: Cartera Europea de Identidad Digital y firma cualificada eIDAS2.
- **Poder de representación** — si quien firma puede obligar a la sociedad. Hoy
  se hace constar en el título por medios convencionales; su comprobación sigue
  ocurriendo fuera de la plataforma. Horizonte: credencial verificable de fuente
  registral vía European Business Wallet (propuesta COM(2025) 838, aún en
  tramitación) y poder de representación digital de la Directiva (UE) 2025/25, no
  aplicable en lo sustancial hasta el 1 de agosto de 2028.

**El pagaré de empresa.** El modelo admite que el firmante sea persona jurídica:
`Nombre` y `NIF` pasan a ser la razón social y el CIF —siguen nombrando al
obligado cambiario— y un `Representante` recoge a la persona física que firma
por ella, con su cargo. Una sociedad no firma; firma alguien por ella.

El artículo 9 LCCH no es aquí un requisito formal sino la frontera de quién
queda obligado: quien firma en nombre de otro debe hallarse autorizado y
**expresarlo claramente en la antefirma**, y quien lo hace sin poder queda
obligado personalmente por el título. De ahí que el cargo sea obligatorio, que el
anverso del PDF lleve la antefirma «P.p. …», y que la falta de acreditación del
poder se advierta —sin privar de validez al título, porque no es la plataforma
quien puede juzgar si el poder existe—.

La acreditación es deliberadamente **fina**: un tipo de documento en texto libre,
una referencia y una fecha. Está pensada para sustituirse por la credencial
verificable del ecosistema europeo, y cualquier estructura más elaborada que se
inventase ahora difícilmente coincidiría con la que llegue, si llega.

Una cautela de implementación que conviene no perder: estos campos entran en la
forma canónica **sólo cuando constan**. Incluirlos siempre habría cambiado la
forma canónica de todos los pagarés ya firmados, y sus firmas habrían dejado de
validar: el sistema los habría dado por alterados.

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

## 6. La entrega del control al beneficiario

En el título en papel la posesión pasa al tenedor por la **entrega**. Su
equivalente electrónico es la transferencia del control, y sin ella el requisito
(iii) del método fiable —control exclusivo del portador— no quedaría satisfecho:
el emisor conservaría el registro y el beneficiario no tendría nada.

La emisión, por tanto, son **dos operaciones**. Se comprobó contra la red que
`POST /asset` crea todo asset a nombre de quien firmó la creación e **ignora
cualquier destinatario** indicado en ese momento, tanto dentro de `asset` como en
la raíz de la petición. La entrega es un `PUT /asset` posterior con `to` = clave
del beneficiario (`internal/handler/entrega.go`).

**La entrega debe distinguirse del endoso**, y esto no es cosmético. La red
sobrescribe el `action` que enviamos y registra toda transferencia como
`TRANSFER`, igual que un endoso, aunque **conserva los campos propios** que
añadamos. Sin una marca, el sistema leería la entrega como el primer endoso de la
cadena: el pagaré aparecería como ENDOSADO desde el momento de emitirse y la
entrega se imprimiría en el reverso del PDF como si lo fuera. De ahí el campo
`tipo_operacion: ENTREGA`, que el cálculo del estado y la cadena de endosos
filtran.

**Cuando la entrega no puede completarse** —el beneficiario no está registrado y
no tiene clave, o la red rechaza la transferencia— el pagaré queda *emitido y
pendiente de entrega*. No es un fallo: es el título firmado que aún no ha
cambiado de manos, un estado real también en papel. Lo que sería incorrecto es
darlo por entregado, porque el control seguiría siendo del emisor. La respuesta
de la emisión lo dice expresamente en el bloque `entrega`, y ese estado se
refleja como **PENDIENTE_ENTREGA** en el listado, de modo que el emisor vea que
queda algo por terminar.

**La entrega puede completarse después**, sin que el título pierda su
identidad. De otro modo el control se quedaría en el emisor para siempre y la
única salida sería anular y reemitir bajo un ID nuevo, lo que para un título ya
firmado es un remedio desproporcionado. Solo cabe una vez: en cuanto el pagaré
ha salido de manos del emisor, toda transmisión posterior es un endoso o una
cesión, con el régimen de responsabilidad que cada uno arrastra, y llamarla
entrega permitiría transmitir sin asumirlo.

El destinatario se toma de `to` si el cliente lo aporta y, si no, se resuelve por
el NIF del beneficiario, que es la mención que el artículo 94 exige en el título.

## 7. Integridad: firma del contenido

En la emisión, el firmante firma la **forma canónica** del contenido del pagaré
—las menciones del artículo 94 LCCH, más el aval y las cláusulas— y la firma se
almacena en `firmante.firma_digital`, dentro del propio contenido del asset.

Que la firma viaje dentro del contenido, y no en los metadatos, responde a una
restricción real: el endpoint público de la red devuelve `data` pero no
`metadata`, de modo que una firma en metadatos sería invisible para quien
consulta sin credenciales. Doctrinalmente además encaja: la firma es una mención
del título (artículo 94.7), no un dato externo a él.

**La forma canónica** (`internal/models/canonical.go`) es una lista blanca
explícita, construida con mapas anidados y no a partir de las estructuras Go. Dos
razones. La red **añade campos propios** al `data` almacenado (`app`, `from`,
`namespace`, `token`, `created_at`) que no deben entrar en la firma; y lo que se
firmó no debe desplazarse por un cambio posterior en una etiqueta de estructura.
Quedan fuera también `firmante.firma_digital` —una firma no puede cubrirse a sí
misma— y `firmante.identidad_blockchain`, que nunca debe llegar al registro
porque podría arrastrar una clave privada.

**La verificación** (`internal/handler/verificacion.go`) responde a dos preguntas
distintas y las informa por separado:

- `firmado` — la firma es válida para `data.from`, la clave que la **red**
  registra como creadora del asset, no una clave que declaremos nosotros.
- `integro` — el contenido almacenado hoy, recalculado a forma canónica, coincide
  byte a byte con lo que esa clave firmó.

Recalcular en lugar de confiar en el mensaje que la firma lleva dentro es lo
esencial: el blob firmado contiene su propio mensaje, de modo que verificarlo
aisladamente solo probaría que alguien firmó *algo*. Un pagaré puede estar
firmado y no ser íntegro —alguien alteró un campo tras la emisión— y ése es
justamente el caso del que hay que advertir al tenedor.

El resultado se expone en el endpoint público y en `/pagares/verificar`, de modo
que **un tercero sin cuenta puede comprobarlo**.

**Pero sin identificar a las partes.** La consulta pública devuelve lo que
acredita el título —denominación, importe, vencimiento, cláusulas, estado y el
resultado de la verificación— y omite lo que identifica a quienes están detrás:
nombres, dirección postal y, por entero, la identidad del representante, que no
es parte del crédito. Los NIF salen enmascarados, de modo que quien tiene el
título en la mano puede cotejarlos sin que un desconocido derive de ellos una
identidad.

La razón es que un título valor está hecho para mostrarse, pero en papel leerlo
exige **tenerlo**, mientras que aquí basta con conocer una cadena que viaja en
códigos QR impresos, en URLs y en correos reenviados. Haber visto pasar un
identificador no es poseer el título. Es la misma dirección que el marco
doctrinal apunta al hablar de mover identidad verificable en lugar de datos
identificables (nota 1).

La verificación se calcula sobre el contenido **completo**, que es lo que se
firmó, y sólo después se recorta lo que se devuelve: de otro modo el recorte la
rompería y el desconocido perdería justamente lo único que la vista pública
existe para darle.

**La firma es obligatoria para emitir.** El artículo 94.7 cuenta la firma del
que emite entre las menciones esenciales, y el artículo 95, párrafo 1, priva de
validez como pagaré al documento al que le falte una: un pagaré sin firma no es
un pagaré defectuoso, no es un pagaré. La emisión que no pueda firmarse se
rechaza en lugar de grabarse sin firma.

Esto obligó a resolver antes un supuesto real: **una cuenta puede no tener
clave**. El aprovisionamiento se hace en el registro con criterio best-effort y
su fallo no es fatal, y el administrador inicial (`BootstrapAdmin`) ni siquiera
lo intenta. Rechazar sin más habría dejado a esas cuentas sin poder emitir, así
que la emisión **aprovisiona la clave que falte** y solo rechaza si tampoco eso
es posible.

Los pagarés emitidos antes de esta exigencia siguen en el registro sin firma. La
verificación los informa como «emitido sin firma del contenido», que es distinto
de «alterado», y conviene que siga siéndolo.

Límite que conviene no perder de vista: esto acredita que el contenido es el que
firmó la clave emisora, no que esa clave corresponda a quien dice ser ni que
tenga poder para obligar a una sociedad. Eso es el §4.

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
| Cláusula «no a la orden» | art. 14 | Modelo, PDF e impedimento efectivo del endoso |
| Endoso: propiedad, blanco, procuración, garantía | arts. 15, 17, 21, 22 | Los cuatro tipos |
| Cláusulas del endoso | arts. 18, 56 | Sin responsabilidad, prohibición de nuevo endoso, sin gastos |
| Aval total y parcial | arts. 35-37 | Con avalado y tope del principal |
| Prescripción | art. 88 | Comprobación periódica (`internal/scheduler`) |
| Representación de persona jurídica | art. 9 | Representante con cargo en la antefirma; aviso si falta acreditación del poder |

Nótese que esta cobertura excede el ámbito de la fase inicial descrita en el
documento doctrinal (§8.3), que se limita a pagarés no a la orden: la
implementación del endoso está hecha, y es el plano institucional —no el
técnico— el que sigue sin resolver la eficacia cambiaria de su circulación
electrónica.

**La cláusula «no a la orden» impide efectivamente el endoso.** La comprobación
se hace consultando el título antes de transferir, y **falla cerrada**: si el
registro no puede leerse, el endoso se rechaza en lugar de dejarse pasar.
Endosar un título no endosable dejaría una cadena de tenedores sobre algo que no
puede circular, un enredo muy costoso de deshacer, mientras que una negativa
durante una incidencia de red no es más que un reintento.

El reverso de esa restricción es **la cesión ordinaria** (artículos 347-348
CCom, en relación con los artículos 1526 y siguientes CC), que es la vía que a
estos pagarés les queda y que la plataforma ofrece como operación propia
(`internal/handler/cesion.go`). Sin ella, impedir el endoso los habría dejado
sin ninguna forma de circular dentro del sistema, y la única salida sería un
documento en papel al margen del registro: justo lo que la infraestructura
existe para evitar.

**La cesión no es un endoso con otro nombre**, y el sistema no debe presentarla
como tal. En el registro las dos operaciones son idénticas —una transferencia—
pero el Derecho que arrastran difiere: el cedente responde de la existencia y
legitimidad del crédito pero **no de la solvencia** del deudor, salvo pacto
(artículo 1529 CC), mientras que el endosante sí responde del pago (artículo 18
LCCH); el deudor conserva frente al cesionario las excepciones que tuviera
contra el cedente, sin la autonomía propia del título cambiario; y la cesión ha
de **notificarse al deudor** para serle oponible, pues hasta entonces el pago al
cedente le libera (artículo 1527 CC).

De ahí que se marque con `tipo_operacion: CESION`, que el estado resultante sea
CEDIDO y no ENDOSADO, y que el reverso del PDF la presente bajo epígrafe propio
y no dentro de la cadena de endosos: incluirla allí atribuiría al cedente una
responsabilidad que nunca asumió. La constancia de la notificación se registra
—fecha y medio—, aunque la notificación misma ocurra fuera del sistema; cuando
no consta, la respuesta lo advierte expresamente.

**La cesión se ofrece en todo pagaré**, no solo en el «no a la orden». En éste es
la única vía; en los demás es una alternativa al endoso, y no una figura ajena al
Derecho cambiario: el propio artículo 24 LCCH dispone que el endoso posterior al
protesto, o al plazo para levantarlo, produce «sólo los efectos de una cesión
ordinaria».

Ahora bien, elegir la cesión sobre un pagaré endosable tiene una **asimetría**
que la interfaz hace explícita, porque no la soporta quien decide: el cedente se
libera de la responsabilidad del artículo 18 LCCH, mientras que es el adquirente
quien pierde —la inoponibilidad de excepciones y la acción de regreso contra
quien le transmitió—. Quien pulsa el botón se beneficia y el perjuicio recae en
la otra parte, de modo que el sistema advierte de lo que el adquirente deja de
recibir y ofrece endosar en su lugar.

## 9. Resumen de lo pendiente

| Plano | Pendiente | Depende de |
|---|---|---|
| Institucional | Presunción legal de unicidad (§3) | Cualificación del prestador |
| Institucional | Firma cualificada eIDAS y cartera de identidad (§4) | Despliegue EUDI Wallet |
| Institucional | Representación societaria verificable (§4) | COM(2025) 838; Directiva 2025/25 (2028) |
| Legislativo | Eficacia cambiaria plena y acceso al juicio cambiario (§3) | Reforma LCCH + LEC |
