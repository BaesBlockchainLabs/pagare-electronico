// Package apidocs reads the OpenAPI specs from disk and turns them into
// something the service can serve: the raw YAML, and a readable index of the
// endpoints.
//
// The spec is rendered server-side rather than by an embedded viewer. Redoc or
// Scalar would look richer, but each is over a megabyte of minified JavaScript
// vendored into the repository, and an endpoint index built from the spec
// itself cannot drift out of date, needs no bundle, and matches the rest of the
// site.
package apidocs

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Spec is the part of an OpenAPI document this package renders.
type Spec struct {
	Titulo      string
	Descripcion string
	Version     string
	Fichero     string
	Grupos      []Grupo
	Rutas       int
	Esquemas    int
}

// Grupo collects the operations sharing a tag.
type Grupo struct {
	Nombre      string
	Descripcion string
	Operaciones []Operacion
}

// Operacion is one method on one path.
type Operacion struct {
	Metodo      string
	Ruta        string
	Resumen     string
	Descripcion string
	Publica     bool // sin autenticación
}

var metodos = []string{"get", "post", "put", "delete", "patch"}

// Cargar reads and interprets a spec file.
func Cargar(ruta string) (*Spec, error) {
	raw, err := os.ReadFile(ruta)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer %s: %w", ruta, err)
	}
	var doc struct {
		Info struct {
			Title       string `yaml:"title"`
			Description string `yaml:"description"`
			Version     string `yaml:"version"`
		} `yaml:"info"`
		Tags []struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		} `yaml:"tags"`
		Paths      map[string]map[string]operacionYAML `yaml:"paths"`
		Components struct {
			Schemas map[string]interface{} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s no es un OpenAPI legible: %w", ruta, err)
	}

	porTag := map[string][]Operacion{}
	total := 0
	for p, ops := range doc.Paths {
		for _, m := range metodos {
			op, ok := ops[m]
			if !ok {
				continue
			}
			total++
			tag := "Otros"
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			porTag[tag] = append(porTag[tag], Operacion{
				Metodo:      m,
				Ruta:        p,
				Resumen:     op.Summary,
				Descripcion: op.Description,
				// security: [] anula la seguridad global: la operación es pública.
				Publica: op.Security != nil && len(*op.Security) == 0,
			})
		}
	}

	spec := &Spec{
		Titulo: doc.Info.Title, Descripcion: doc.Info.Description,
		Version: doc.Info.Version, Fichero: ruta,
		Rutas: total, Esquemas: len(doc.Components.Schemas),
	}

	// Los tdeclarados primero y en su orden, que es el que da sentido al
	// recorrido; lo que no esté declarado, detrás y alfabético.
	vistos := map[string]bool{}
	añade := func(nombre, desc string) {
		ops := porTag[nombre]
		if len(ops) == 0 {
			return
		}
		sort.Slice(ops, func(i, j int) bool {
			if ops[i].Ruta != ops[j].Ruta {
				return ops[i].Ruta < ops[j].Ruta
			}
			return ops[i].Metodo < ops[j].Metodo
		})
		spec.Grupos = append(spec.Grupos, Grupo{Nombre: nombre, Descripcion: desc, Operaciones: ops})
		vistos[nombre] = true
	}
	for _, t := range doc.Tags {
		añade(t.Name, t.Description)
	}
	var resto []string
	for nombre := range porTag {
		if !vistos[nombre] {
			resto = append(resto, nombre)
		}
	}
	sort.Strings(resto)
	for _, nombre := range resto {
		añade(nombre, "")
	}

	return spec, nil
}

type operacionYAML struct {
	Tags        []string       `yaml:"tags"`
	Summary     string         `yaml:"summary"`
	Description string         `yaml:"description"`
	Security    *[]interface{} `yaml:"security"`
}
