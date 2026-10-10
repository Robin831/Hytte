// Command trips-import creates a trip from a JSON file in the trip import
// format (e.g. one converted from an older trip page). Personal data: keep
// the files out of the repository.
//
//	trips-import -db /home/robin/Hytte/hytte.db -user 1 -file trip.json [-dry-run]
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

	"github.com/Robin831/Hytte/internal/db"
	"github.com/Robin831/Hytte/internal/trips"
)

func main() {
	dbPath := flag.String("db", "hytte.db", "path to the Hytte database")
	userID := flag.Int64("user", 0, "owner user id")
	file := flag.String("file", "", "trip JSON file")
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
	var f trips.ImportFile
	if err := json.Unmarshal(raw, &f); err != nil {
		log.Fatalf("parse %s: %v", *file, err)
	}
	in := trips.TripInput{Kind: f.Kind, StartDate: f.StartDate, EndDate: f.EndDate, HomeTZ: f.HomeTZ, DestTZ: f.DestTZ, Doc: f.Doc}
	if err := in.Normalize(); err != nil {
		log.Fatalf("invalid trip: %v", err)
	}
	fmt.Printf("%q: %s %s–%s, %d flights, %d stays, %d days, %d checklists\n", f.Title["en"], f.Kind, f.StartDate, f.EndDate,
		len(f.Flights), len(f.Stays), len(f.Days), len(f.Checklists))
	if *dryRun {
		return
	}
	database, err := db.Init(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()
	id, err := trips.ImportTrip(context.Background(), database, *userID, f)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	fmt.Printf("created trip %d\n", id)
}
