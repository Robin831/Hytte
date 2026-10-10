// Command races-import adds a JSON file of past race results (e.g. a Gmail
// backfill) to a user's hall of fame as pending results, for them to review
// and confirm in the app. It goes through the same validation as the API.
//
//	races-import -db /home/robin/Hytte/hytte.db -user 1 -file results.json [-dry-run]
//
// Run it as the same OS user as the server so the encryption key file is
// found (or set ENCRYPTION_KEY).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/races"
)

func main() {
	dbPath := flag.String("db", "hytte.db", "path to the Hytte database")
	userID := flag.Int64("user", 0, "user id to import for")
	file := flag.String("file", "", "JSON file with an array of results")
	dryRun := flag.Bool("dry-run", false, "parse and validate only")
	flag.Parse()
	if *userID <= 0 || *file == "" {
		flag.Usage()
		os.Exit(2)
	}

	raw, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("read %s: %v", *file, err)
	}
	var cands []races.ImportCandidate
	if err := json.Unmarshal(raw, &cands); err != nil {
		log.Fatalf("parse %s: %v", *file, err)
	}
	fmt.Printf("%d candidates in %s\n", len(cands), *file)
	if *dryRun {
		return
	}

	database, err := db.Init(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	var name string
	if err := database.QueryRow(`SELECT name FROM users WHERE id = ?`, *userID).Scan(&name); err != nil {
		log.Fatalf("user %d: %v", *userID, err)
	}
	first := strings.Fields(name)
	self := ""
	if len(first) > 0 {
		self = first[0]
	}
	added, skipped, errs := races.ImportResults(context.Background(), database, *userID, self, cands)
	fmt.Printf("added %d, skipped %d duplicates, %d errors\n", added, skipped, len(errs))
	for _, e := range errs {
		fmt.Println("  error:", e)
	}
}
