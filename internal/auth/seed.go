package auth

import (
	"fmt"
	"log"
	"math/rand"
)

// seedPassword is the shared, well-known password for development seed users so
// they can be logged into during testing. Never used outside dev seeding.
const seedPassword = "seed1234"

var (
	seedNombres   = []string{"María", "Juan", "Ana", "Carlos", "Laura", "Pedro", "Elena", "Miguel", "Isabel", "Javier"}
	seedApellidos = []string{"García", "Fernández", "López", "Martínez", "Sánchez", "Pérez", "Gómez", "Martín", "Jiménez", "Ruiz"}
	seedCiudades  = []string{"Madrid", "Barcelona", "Valencia", "Sevilla", "Bilbao", "Zaragoza", "Málaga", "Murcia", "Palma", "A Coruña"}
	seedCalles    = []string{"Calle Mayor 10", "Av. de la Constitución 25", "Paseo del Prado 8", "Calle Gran Vía 42", "Rambla de Cataluña 15", "Calle Alcalá 100"}
)

// SeedDevUsers provisions up to n development users (usernames seed001..seedNNN)
// with plausible Spanish data — mirroring the old "Datos de prueba" button — and
// a real blockchain keypair each. Idempotent: existing seed users are skipped
// (and their keypair ensured). Returns how many new users were created.
//
// Requires a keyvault and a key provisioner to be wired. Intended for
// development only; the caller must gate it on the environment.
func (s *Store) SeedDevUsers(n int) (created int, err error) {
	if s.vault == nil {
		return 0, ErrNoVault
	}
	if s.keygen == nil {
		return 0, ErrNoKeyProvisioner
	}
	// Deterministic generator so repeated seeding yields stable data.
	rng := rand.New(rand.NewSource(1))

	for i := 1; i <= n; i++ {
		username := fmt.Sprintf("seed%03d", i)

		// Skip if the user already exists, but make sure it has a keypair.
		if existing, _ := s.getByUsername(username); existing != nil {
			if _, kerr := s.EnsureUserKeypair(existing.ID); kerr != nil {
				log.Printf("[seed] %s: no se pudo asegurar keypair: %v", username, kerr)
			}
			continue
		}

		u := &User{
			Username:     username,
			Role:         RoleUser,
			Nombre:       seedNombres[rng.Intn(len(seedNombres))],
			Apellido:     seedApellidos[rng.Intn(len(seedApellidos))],
			NIF:          randNIF(rng),
			Direccion:    seedCalles[rng.Intn(len(seedCalles))],
			Localidad:    seedCiudades[rng.Intn(len(seedCiudades))],
			CodigoPostal: fmt.Sprintf("%05d", 10000+rng.Intn(89999)),
			Pais:         "ES",
		}
		u.DisplayName = u.Nombre + " " + u.Apellido

		if err := s.CreateUser(u, seedPassword); err != nil {
			if err == ErrUserAlreadyExists {
				continue
			}
			return created, fmt.Errorf("creando %s: %w", username, err)
		}
		if _, err := s.EnsureUserKeypair(u.ID); err != nil {
			return created, fmt.Errorf("keypair de %s: %w", username, err)
		}
		created++
	}
	return created, nil
}

// getByUsername returns a user by username, or (nil, nil) if not found.
func (s *Store) getByUsername(username string) (*User, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id)
	if err != nil {
		return nil, nil
	}
	return s.GetByID(id)
}

// randNIF builds a valid-format Spanish DNI (8 digits + control letter).
func randNIF(rng *rand.Rand) string {
	n := 10000000 + rng.Intn(89999999)
	const letters = "TRWAGMYFPDXBNJZSQVHLCKE"
	return fmt.Sprintf("%d%c", n, letters[n%23])
}
