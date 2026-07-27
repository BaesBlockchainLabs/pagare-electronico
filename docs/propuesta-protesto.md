# Propuesta: el protesto y el endoso tardío (art. 24 LCCH)

Propuesta de diseño para resolver la incidencia
[#10](https://github.com/BaesBlockchainLabs/pagare-electronico/issues/10),
que quedó bloqueada porque el protesto no está modelado.

> Documento de decisión, no de implementación. Contiene un punto que conviene
> confirmar con la dirección doctrinal del proyecto antes de escribir código;
> está marcado como **[A confirmar]**.

## 1. El problema

El artículo 24 LCCH dispone que el endoso **posterior al protesto o declaración
equivalente por falta de pago, o al vencimiento del plazo establecido para
levantar el protesto**, no produce otros efectos que los de una cesión ordinaria.

Es decir: pasado cierto momento, la operación se sigue llamando endoso y se sigue
firmando como tal, pero lo que transmite es lo que transmite una cesión —el
crédito con sus defectos, sin responsabilidad del transmitente por la solvencia—.

Hoy la plataforma no lo tiene en cuenta. Un tenedor puede endosar un pagaré
vencido creyendo que transmite un derecho autónomo, y no es así. La diferencia
con el aviso que ya damos en la pantalla de cesión importa: allí el régimen débil
**se elige**; aquí se aplica **por ministerio de la ley**, sin que nadie lo sepa,
y la operación conserva el nombre de endoso.

Perjudica a las dos partes en direcciones opuestas. El endosante cree quedar
obligado como responsable de regreso y no queda —puede estar cobrando por algo
que no entrega—; el endosatario cree adquirir un derecho limpio y adquiere el
crédito con todas sus excepciones.

## 2. El hallazgo que desbloquea la incidencia

El artículo 24 tiene **dos disparadores**, y sólo uno de ellos necesita el
protesto:

| Disparador | Qué hace falta para detectarlo |
|---|---|
| (a) Endoso posterior al **protesto** o declaración equivalente | Modelar el protesto |
| (b) Endoso posterior al **vencimiento del plazo** para levantarlo | Sólo la fecha de vencimiento |

El segundo no exige que haya habido protesto: basta con que el plazo haya
transcurrido. Y ese plazo es el del artículo 51, párrafo cuarto —**ocho días
hábiles siguientes al del vencimiento**—, calculable a partir de datos que ya
tenemos.

En la práctica, (b) cubre la mayoría de los casos: los pagarés que se endosan
tarde suelen serlo mucho después del vencimiento, sin que nadie haya levantado
protesto. La incidencia, por tanto, **no está realmente bloqueada**: puede
resolverse en su mayor parte sin modelar el protesto.

De ahí la propuesta de dividirla.

## 3. Propuesta de división

### 10a — Aviso por plazo transcurrido (sin protesto)

Detectar el disparador (b) y advertirlo en la pantalla de endoso, con el mismo
criterio que ya se aplica en la de cesión: decir qué recibe realmente el
adquirente y qué responsabilidad deja de asumir el endosante.

No toca el modelo ni el registro. Es la parte barata y la que más engaño evita.

### 10b — Modelado del protesto

Registrar el protesto como operación propia, lo que habilita el disparador (a) y,
de paso, cubre una laguna independiente: hoy no hay forma de dejar constancia de
que un pagaré fue protestado.

Depende de decisiones que conviene tomar antes (§5).

## 4. El cálculo del plazo, y su dificultad real

«Ocho días hábiles» exige un calendario laboral. Y el calendario depende del
lugar de pago: a las fiestas nacionales se suman las autonómicas y las locales,
que la plataforma no conoce ni tiene por qué conocer.

**Propuesta: no fingir precisión que no tenemos.** En lugar de una fecha exacta,
calcular dos:

- **Plazo mínimo** — contando como inhábiles sólo sábados y domingos. Es lo antes
  que el plazo puede haber vencido.
- **Plazo máximo** — añadiendo un margen por festivos posibles.

Y avisar en tres tramos:

| Momento | Aviso |
|---|---|
| Antes del plazo mínimo | Ninguno: el endoso es pleno |
| Entre mínimo y máximo | «El plazo **puede haber** transcurrido»: el endoso podría producir efectos de cesión |
| Después del máximo | «El plazo ha transcurrido»: el endoso produce efectos de cesión |

Esto es preferible a una fecha exacta falsa por una razón asimétrica: **avisar de
más molesta; avisar de menos engaña**. Quien recibe un aviso prematuro puede
seguir adelante; quien no lo recibe cuando debía, cree estar haciendo algo que no
hace.

**Pagarés a la vista.** El artículo 51.4 fija el plazo para los pagaderos en
fecha fija o a cierto plazo. Para los pagaderos a la vista la regla es distinta y
se enreda con el plazo de presentación del artículo 39. **Propongo dejarlos
fuera de 10a** y no avisar sobre ellos, antes que avisar mal; se documenta la
exclusión.

## 5. Modelado del protesto (10b)

### Qué es

Para el pagaré sólo cabe el **protesto por falta de pago** —no hay aceptación que
protestar, porque el firmante es ya el obligado principal (art. 97: queda
obligado de igual manera que el aceptante de una letra)—.

Se levanta por **acta notarial** (art. 51.1), pero el artículo 51.2 admite una
**declaración equivalente**: la que consta en el propio título, firmada y fechada
por quien deniega el pago, o la del domiciliatario o la Cámara de Compensación.

### Qué registrar

Operación propia, marcada `tipo_operacion: PROTESTO`, que **no transfiere el
control** —es una actualización de metadatos, no una transmisión—:

- `tipo`: `notarial` | `declaracion_equivalente`
- `fecha`: la del acta o la declaración
- `referencia`: protocolo notarial, o identificación de la declaración
- `declarante`: en la declaración equivalente, quién la firma (firmante,
  domiciliatario, cámara)

Quien puede registrarlo es el **tenedor actual**, que es quien lo levanta.

Estado resultante: **PROTESTADO**, visible como los demás.

### Lo que el protesto decide, y lo que no

Conviene que la interfaz no exagere su efecto. La falta de protesto hace perder
las acciones de **regreso** contra endosantes y avalistas (art. 63), pero **no la
acción directa contra el firmante** del pagaré, que responde como aceptante
(art. 97). Un pagaré no protestado no es un pagaré perdido: es un pagaré cuya
cadena de regreso se ha desactivado.

### La cláusula «sin gastos» **[A confirmar]**

El artículo 56 permite dispensar al tenedor de levantar protesto para conservar
sus acciones de regreso, y el modelo ya contempla esa cláusula.

Mi lectura es que **la cláusula no detiene el reloj del artículo 24**: dispensa
del *acto* de protestar, no del *plazo* que el artículo 24 toma como referencia
temporal. El plazo del artículo 51.4 existe con independencia de que uno esté
dispensado de agotarlo, y el artículo 24 lo usa como marca del momento en que el
endoso deja de ser pleno.

Si esa lectura es correcta, un pagaré «sin gastos» endosado tarde produce
igualmente efectos de cesión, y el aviso debe darse igual. Si no lo es, habría
que suprimir el aviso cuando conste la cláusula.

**Es el punto que propongo confirmar antes de implementar**, porque decide el
comportamiento en un caso frecuente —la cláusula «sin gastos» es habitual en los
pagarés de empresa— y porque una interpretación equivocada haría que el sistema
avisara de un efecto que no se produce, o callara uno que sí.

## 6. Lo que no propongo hacer

- **Bloquear el endoso tardío.** El artículo 24 no lo prohíbe: le cambia los
  efectos. Impedirlo sería inventar una restricción que la ley no impone.
- **Renombrar la operación a «cesión».** Sigue siendo un endoso en su forma y en
  su firma; lo que cambia es su alcance. Llamarlo de otro modo confundiría el
  historial.
- **Calcular festivos locales.** Requeriría un calendario por municipio que
  envejecería mal y daría una precisión aparente que no sostiene.
- **Avisar en pagarés a la vista**, mientras no se resuelva su plazo (§4).

## 7. Orden sugerido

1. Confirmar el punto de §5 sobre la cláusula «sin gastos».
2. Implementar **10a**: aviso por plazo, con los tres tramos. Sin tocar el
   modelo.
3. Decidir si **10b** merece la pena por sí mismo. Registrar el protesto tiene
   valor propio —hoy no hay dónde dejar constancia— pero su aportación al
   artículo 24 es marginal frente a 10a.
