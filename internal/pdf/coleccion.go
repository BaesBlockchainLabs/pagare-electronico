package pdf

import "regexp"

// arbolVacioDeFicheros localiza la entrada /Names del catálogo cuando su árbol
// /EmbeddedFiles está vacío. fpdf la escribe siempre, aunque el documento no
// lleve ningún adjunto, y no ofrece manera de desactivarla.
//
// Exige que la lista esté vacía a propósito: un árbol con adjuntos de verdad no
// se toca, porque borrarlo perdería los ficheros. Hoy no adjuntamos nada; el día
// que se adjunte, el envío volverá a fallar y será por un motivo real.
var arbolVacioDeFicheros = regexp.MustCompile(
	`(?s)/Names\s*<<\s*/EmbeddedFiles\s*<<\s*/Names\s*\[\s*\]\s*>>\s*>>`)

// sinColeccion deja el PDF sin el árbol de ficheros incrustados vacío.
//
// Un PDF con /EmbeddedFiles es, para quien lo lea, una colección de documentos
// —un portafolio— aunque el árbol venga vacío. Logalty lo clasifica así y su
// core rechaza el envío con el código 150, "Error enviando a core", que no dice
// nada de esto; en el portal el envío aparece como "colección de PDF".
//
// Se sustituye por espacios en lugar de recortarlo, para que no se mueva ningún
// byte: los offsets de la tabla xref y el startxref siguen siendo válidos sin
// tener que recalcularlos. El diccionario del catálogo sigue bien formado,
// simplemente con una entrada menos.
func sinColeccion(documento []byte) []byte {
	return arbolVacioDeFicheros.ReplaceAllFunc(documento, func(coincidencia []byte) []byte {
		espacios := make([]byte, len(coincidencia))
		for i := range espacios {
			espacios[i] = ' '
		}
		return espacios
	})
}
