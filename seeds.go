package main

import (
	"fmt"
	"strings"
)

// seedBirds is project-owned catalog content. Images are added in Checkpoint 4.
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

// seedSpecies can run on every startup. Existing catalog rows keep their IDs
// and any later edits because common_name is unique and conflicts are ignored.
func (s *Store) seedSpecies() error {
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
	return tx.Commit()
}
