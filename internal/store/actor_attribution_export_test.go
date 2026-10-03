package store

import "context"

// ActorAttributionForTest keeps test-only SQL assertions in the store package.
func (s *Store) ActorAttributionForTest(ctx context.Context, itemID string, snapshotSeq int, destinations ...any) error {
	if snapshotSeq == 0 {
		return s.db.QueryRowContext(ctx,
			"SELECT created_by,updated_by FROM items WHERE id=?", itemID).Scan(destinations...)
	}
	if len(destinations) == 1 {
		return s.db.QueryRowContext(ctx,
			"SELECT published_by FROM snapshots WHERE item_id=? AND seq=?", itemID, snapshotSeq).Scan(destinations...)
	}
	return s.db.QueryRowContext(ctx, `
		SELECT i.created_by,i.updated_by,s.published_by
		FROM items i JOIN snapshots s ON s.id=i.published_snapshot_id
		WHERE i.id=? AND s.seq=?`, itemID, snapshotSeq).Scan(destinations...)
}

func (s *Store) ItemCountForTest(ctx context.Context, destination *int) error {
	return s.db.QueryRowContext(ctx, "SELECT count(*) FROM items").Scan(destination)
}
