# Forma legal del pagaré (referencia para la impresión en PDF)

Documento de referencia para diseñar e implementar la "impresión" del pagaré en
PDF (anverso + reverso). Base normativa: **Ley 19/1985, de 16 de julio, Cambiaria
y del Cheque (LCCH)**, arts. 94‑97 (pagaré) con remisión a las normas de la letra
de cambio (art. 96).

> Nota: referencia interna para el producto, no asesoramiento jurídico.

## 1. Forma libre

El pagaré **no requiere papel timbrado ni formato oficial**: la ley da libertad de
forma y redacción siempre que consten las menciones del art. 94 (y las suplencias
del art. 95). Los pagarés de empresa usan formato libre, habitualmente inspirado
en el modelo bancario.

## 2. Anverso — menciones obligatorias (art. 94)

| # | Mención | Campo en el modelo (`PagareElectronico`) |
|---|---------|------------------------------------------|
| 1 | Denominación **«pagaré»** inserta en el texto y en el idioma de redacción | `Denominacion` (= "PAGARÉ") |
| 2 | **Promesa pura y simple** de pagar una cantidad determinada (EUR o divisa convertible cotizada) | `PromesaPago` (true), `Importe`, `Moneda` |
| 3 | **Vencimiento** | `Vencimiento{Tipo, Fecha}` (`fecha_fija` / `a_la_vista`) |
| 4 | **Lugar de pago** | `LocalidadPago` |
| 5 | Persona a quien (o a **cuya orden**) se ha de pagar — beneficiario/tenedor | `Beneficiario{Nombre, Apellido, NIF}` |
| 6 | **Fecha y lugar de emisión** (libramiento) | `FechaEmision`, `LocalidadEmision` |
| 7 | **Firma** del que emite (firmante/suscriptor) | `Firmante{...}` + firma (en la plataforma, respaldo blockchain) |

### Suplencias (art. 95) — cómo rellenar si falta una mención

- **Sin vencimiento** ⇒ pagadero **a la vista**.
- **Sin lugar de pago** ⇒ el **lugar de emisión** (que se reputa además domicilio del firmante).
- **Sin lugar de emisión** ⇒ el que figure **junto al nombre del firmante**.

(La ausencia de cualquier otra mención del art. 94, sin suplencia aplicable, priva
al documento de su validez como pagaré — art. 95 párr. 1.)

### Importe en cifra y en letra (art. 7)

Conviene expresar el importe **en cifra y en letra**. En caso de divergencia
**prevalece lo escrito en letra**; si hay varias indicaciones que difieren, vale
la **menor**. ⇒ El PDF debe incluir el importe en letra (conversión número→texto
en español).

### Cláusulas del anverso (opcionales)

- **«No a la orden»** (art. 14): impide el endoso; solo cesión ordinaria. `NoALaOrden`.
- **«Sin gastos» / «sin protesto»** (art. 56): dispensa del protesto.
- **Aval** (arts. 35‑37): puede constar en el anverso; **la sola firma de un
  tercero en el anverso vale como aval** (salvo que sea la del firmante). `Aval{...}`.

## 3. Reverso — endosos, avales y cláusulas

El **endoso** debe escribirse en el pagaré (habitualmente al **dorso**) o en su
suplemento/anexo si no hay espacio, y ser **firmado por el endosante** (arts. 16‑17,
aplicables por el art. 96).

- **Endoso pleno / en propiedad**: fórmula tipo *«Páguese a la orden de
  [endosatario], [NIF]»*, fecha y firma del endosante. (`en_propiedad`)
- **Endoso en blanco** (art. 15): **solo la firma** del endosante (sin designar
  endosatario); puede ir al dorso. (`en_blanco`)
- **Endoso en procuración/cobranza** (art. 21): «valor al cobro», «para cobranza»,
  «por poder». (`en_procuracion`)
- **Endoso en garantía/prenda** (art. 22): «valor en garantía», «valor en prenda».
  (`en_garantia`)
- Cláusulas del endoso: **«sin mi responsabilidad»** (art. 18), **prohibición de
  nuevo endoso** (art. 18), **«sin gastos»** (art. 56).

**Aval** que no quepa en el anverso: al dorso o en anexo, indicando *«por aval de
[avalado]»* (a falta de indicación, se entiende avalado el firmante — art. 36) y,
si es parcial, el importe avalado. `Aval{Avalista, Alcance, ImporteParcial, Avalado}`.

La **cadena de endosos** debe reflejarse ordenada (del primero al último tenedor)
para acreditar la legitimación del tenedor por una serie no interrumpida de endosos
(art. 19). En la plataforma esta cadena se obtiene del historial del asset en BCF
(`GetAssetHistory`: `CREATE` → `TRANSFER`/endosos → `BURN`).

## 4. Estructura propuesta del PDF

**Página 1 — Anverso** (una hoja): cabecera con la denominación **PAGARÉ**;
promesa de pago con **importe en cifra y en letra**; **vencimiento**; **«Páguese a
[beneficiario]»**; **lugar y fecha de emisión**; datos y **firma del firmante**;
bloque de **aval/cláusulas** si aplican; y **sello/QR de verificación blockchain**
(ID del asset + enlace a `/pagares/verificar`) como valor añadido de autenticidad.

**Página 2 — Reverso**: **cadena de endosos** (bloques/filas con fórmula,
endosatario, fecha y cláusula; endoso en blanco = solo firma), **avales**
adicionales y **notas/cláusulas**. Escalable a varios endosos; remitir a anexo si
excede.

## 5. Prescripción (contexto, art. 88 y 96)

Las acciones cambiarias contra el firmante prescriben a los **3 años** desde el
vencimiento. Relevante para el estado del pagaré, no una mención del documento.
