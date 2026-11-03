package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// seedBirds is project-owned catalog content.
var seedBirds = []struct{ common, scientific, description, habitat string }{
	{"American Goldfinch", "Spinus tristis", "A small finch with a bouncing flight and bright breeding plumage.", "Fields, gardens, and open areas with seed-bearing plants."},
	{"American Robin", "Turdus migratorius", "A familiar thrush often seen searching lawns for food.", "Lawns, parks, woodland edges, and neighborhoods."},
	{"Black-capped Chickadee", "Poecile atricapillus", "A small, active songbird with a black cap and white cheeks.", "Woodlands, parks, and tree-filled neighborhoods."},
	{"Canada Goose", "Branta canadensis", "A large waterfowl with a black neck and white cheek patch.", "Lakes, rivers, wetlands, and grassy fields."},
	{"Dark-eyed Junco", "Junco hyemalis", "A ground-feeding sparrow commonly seen in cooler months.", "Woodlands, shrubs, parks, and yards."},
	{"European Starling", "Sturnus vulgaris", "A social songbird with glossy, speckled plumage.", "Open country, farms, cities, and towns."},
	{"House Finch", "Haemorhous mexicanus", "A compact finch; males often show red on the head and chest.", "Neighborhoods, parks, and dry open country."},
	{"Mallard", "Anas platyrhynchos", "A widespread duck commonly seen swimming and dabbling.", "Ponds, lakes, rivers, and wetlands."},
	{"Mourning Dove", "Zenaida macroura", "A slender dove with a long pointed tail and soft call.", "Open woodland, fields, parks, and neighborhoods."},
	{"Northern Flicker", "Colaptes auratus", "A large woodpecker that often searches for ants on the ground.", "Open woods, parks, and areas with scattered trees."},
	{"Red-tailed Hawk", "Buteo jamaicensis", "A broad-winged hawk often seen soaring or perched above roads.", "Open country, woodland edges, and fields."},
	{"Song Sparrow", "Melospiza melodia", "A streaked sparrow known for its varied song.", "Shrubby edges, wetlands, gardens, and fields."},
}

//go:embed static/images/species/credits.json
var speciesPhotoCredits []byte

type speciesPhotoCredit struct {
	CommonName string `json:"common_name"`
	ImagePath  string `json:"image_path"`
	Author     string `json:"author"`
	SourceURL  string `json:"source_url"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
}

// seedSpecies can run on every startup. Existing catalog rows keep their IDs
// and any later edits because common_name is unique and conflicts are ignored.
func (s *Store) seedSpecies() error {
	var credits []speciesPhotoCredit
	if err := json.Unmarshal(speciesPhotoCredits, &credits); err != nil {
		return fmt.Errorf("read species photo credits: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, bird := range seedBirds {
		// Cornell's species account provides a reference link, not an image asset.
		slug := strings.ReplaceAll(bird.common, " ", "_")
		_, err = tx.Exec(`INSERT INTO species
			(common_name, scientific_name, description, habitat, source_name, source_url)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(common_name) DO NOTHING`,
			bird.common, bird.scientific, bird.description, bird.habitat,
			"Cornell Lab All About Birds", "https://www.allaboutbirds.org/guide/"+slug)
		if err != nil {
			return fmt.Errorf("seed %s: %w", bird.common, err)
		}
	}
	// Upgrade existing catalogs without replacing their identities or descriptions.
	for _, photo := range credits {
		if _, err := tx.Exec(`UPDATE species SET image_path = ?, image_author = ?, image_source_url = ?, image_license = ?, image_license_url = ?
		 WHERE common_name = ? AND (image_path IS NULL OR image_path = '')`, photo.ImagePath, photo.Author, photo.SourceURL, photo.License, photo.LicenseURL, photo.CommonName); err != nil {
			return fmt.Errorf("seed photo %s: %w", photo.CommonName, err)
		}
	}
	return tx.Commit()
}
