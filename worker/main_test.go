package main

// Test unitario para la condición verificable de la sección 6 del enunciado:
// "Bundle incompleto: ante la ausencia de index.md o log.md, la validación
// falla, el bundle no se publica y no se habilita su descarga."
//
// Como el flujo normal (buildBundle) siempre arma un bundle bien formado,
// no hay forma de disparar esta condición subiendo un documento cualquiera.
// Por eso la probamos directamente contra validateBundle(), armando bundles
// "a mano" (buenos y rotos) en memoria, sin pasar por Redis/Postgres/MinIO.

import (
	"archive/zip"
	"bytes"
	"testing"
)

// buildZip es una ayuda de prueba: recibe un mapa nombre->contenido y arma
// un .zip en memoria, igual que hace buildBundle() en el código real.
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatalf("no se pudo crear %s en el zip de prueba: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("no se pudo escribir %s en el zip de prueba: %v", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("no se pudo cerrar el zip de prueba: %v", err)
	}
	return buffer.Bytes()
}

func TestValidateBundle_BundleValido(t *testing.T) {
	bundle := buildZip(t, map[string]string{
		"index.md":     "# Bundle\n\n- [Unidad 1](documento.md)\n",
		"log.md":       "# Conversion log\n\n- Unidades detectadas: 1\n",
		"documento.md": "contenido del concepto",
	})

	if err := validateBundle(bundle); err != nil {
		t.Errorf("un bundle completo y bien formado no debería fallar la validación, pero falló: %v", err)
	}
}

func TestValidateBundle_FaltaLogMd(t *testing.T) {
	// Caso central de la condición verificable: sin log.md, debe rechazarse.
	bundle := buildZip(t, map[string]string{
		"index.md":     "# Bundle\n\n- [Unidad 1](documento.md)\n",
		"documento.md": "contenido del concepto",
	})

	err := validateBundle(bundle)
	if err == nil {
		t.Fatal("un bundle sin log.md debería fallar la validación, pero pasó")
	}
	t.Logf("rechazado correctamente: %v", err)
}

func TestValidateBundle_FaltaIndexMd(t *testing.T) {
	bundle := buildZip(t, map[string]string{
		"log.md":       "# Conversion log\n\n- Unidades detectadas: 1\n",
		"documento.md": "contenido del concepto",
	})

	err := validateBundle(bundle)
	if err == nil {
		t.Fatal("un bundle sin index.md debería fallar la validación, pero pasó")
	}
	t.Logf("rechazado correctamente: %v", err)
}

func TestValidateBundle_SinConceptos(t *testing.T) {
	// index.md y log.md presentes, pero ningún archivo de concepto.
	bundle := buildZip(t, map[string]string{
		"index.md": "# Bundle\n\n(vacío)\n",
		"log.md":   "# Conversion log\n\n- Unidades detectadas: 0\n",
	})

	err := validateBundle(bundle)
	if err == nil {
		t.Fatal("un bundle sin ningún concepto debería fallar la validación, pero pasó")
	}
	t.Logf("rechazado correctamente: %v", err)
}

func TestValidateBundle_EnlaceRoto(t *testing.T) {
	// index.md enlaza a un archivo que no existe dentro del bundle.
	bundle := buildZip(t, map[string]string{
		"index.md":     "# Bundle\n\n- [Unidad 1](no-existe.md)\n",
		"log.md":       "# Conversion log\n\n- Unidades detectadas: 1\n",
		"documento.md": "contenido del concepto",
	})

	err := validateBundle(bundle)
	if err == nil {
		t.Fatal("un bundle con un enlace roto en index.md debería fallar la validación, pero pasó")
	}
	t.Logf("rechazado correctamente: %v", err)
}
