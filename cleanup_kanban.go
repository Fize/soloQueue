package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(home, ".soloqueue", "permanent_memory", "entries.db")
	fmt.Printf("Connecting to SQLite database: %s\n", dbPath)

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Printf("Database file does not exist at %s. Nothing to clean.\n", dbPath)
		return
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	tables := []string{
		"todo_dependencies",
		"todo_items",
		"issue_comments",
		"issue",
	}

	fmt.Println("Dropping deprecated Kanban/Issue tables...")
	for _, table := range tables {
		query := fmt.Sprintf("DROP TABLE IF EXISTS %s;", table)
		_, err := db.Exec(query)
		if err != nil {
			fmt.Printf("  [ERROR] Failed to drop table %s: %v\n", table, err)
		} else {
			fmt.Printf("  [OK] Dropped table %s (if it existed)\n", table)
		}
	}

	fmt.Println("Running VACUUM to compress database size...")
	_, err = db.Exec("VACUUM;")
	if err != nil {
		fmt.Printf("  [ERROR] VACUUM failed: %v\n", err)
	} else {
		fmt.Println("  [OK] VACUUM completed successfully.")
	}

	// Also check if there is an old soloqueue.db in the root ~/.soloqueue
	oldDbPath := filepath.Join(home, ".soloqueue", "soloqueue.db")
	if _, err := os.Stat(oldDbPath); err == nil {
		fmt.Printf("Found legacy database file at: %s\n", oldDbPath)
		fmt.Println("Cleaning deprecated tables in legacy database...")
		oldDb, err := sql.Open("sqlite", oldDbPath)
		if err == nil {
			for _, table := range tables {
				query := fmt.Sprintf("DROP TABLE IF EXISTS %s;", table)
				_, _ = oldDb.Exec(query)
			}
			_, _ = oldDb.Exec("VACUUM;")
			oldDb.Close()
			fmt.Println("  [OK] Legacy database cleaned.")
		}
	}

	fmt.Println("\nCleanup completed successfully! You can run this script using:")
	fmt.Println("  go run cleanup_kanban.go")
	fmt.Println("And safely delete this file or add it to .gitignore when done.")
}
