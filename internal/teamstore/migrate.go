package teamstore

// MigrateFromFiles is a no-op compatibility wrapper now that the teamstore is directly file-backed.
func (s *Store) MigrateFromFiles(groupsDir, agentsDir string) error {
	return nil
}
