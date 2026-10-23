package db

import (
	"context"
	"database/sql"
	"errors"

	"cczjVideo/app/model"
)

// LibraryReadingWindow reads at most three chapter rows. Counts use the existing
// (item_id, ord) index; no complete catalog is allocated on chapter navigation.
type LibraryReadingWindow struct {
	Current, Previous, Next *model.LibraryChapter
	Index, Count, Remaining int
}

func ReadLibraryWindow(ctx context.Context, itemID, chapterID int, remoteID string, ord int, first bool) (*LibraryReadingWindow, error) {
	tx, err := instance.BeginTxx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	window := &LibraryReadingWindow{Index: -1}
	if err := tx.GetContext(ctx, &window.Count, `SELECT COUNT(*) FROM library_chapters WHERE item_id=? AND is_volume=0`, itemID); err != nil {
		return nil, err
	}
	query, args := `SELECT * FROM library_chapters WHERE item_id=? AND is_volume=0`, []any{itemID}
	switch {
	case remoteID != "":
		query += ` AND remote_id=?`
		args = append(args, remoteID)
	case first:
		query += ` AND lifecycle_state='active'`
	case chapterID > 0:
		query += ` AND id=?`
		args = append(args, chapterID)
	case ord >= 0:
		query += ` AND ord=?`
		args = append(args, ord)
	default:
		query += ` AND id=0`
	}
	var current model.LibraryChapter
	err = tx.GetContext(ctx, &current, query+` ORDER BY ord,id LIMIT 1`, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return window, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	window.Current = &current
	if err := tx.GetContext(ctx, &window.Index, `SELECT COUNT(*) FROM library_chapters WHERE item_id=? AND is_volume=0 AND (ord<? OR (ord=? AND id<?))`, itemID, current.Ord, current.Ord, current.Id); err != nil {
		return nil, err
	}
	if err := tx.GetContext(ctx, &window.Remaining, `SELECT COUNT(*) FROM library_chapters WHERE item_id=? AND is_volume=0 AND lifecycle_state='active' AND (ord>? OR (ord=? AND id>?))`, itemID, current.Ord, current.Ord, current.Id); err != nil {
		return nil, err
	}
	for _, direction := range []bool{false, true} {
		op, order := "<", "DESC"
		if direction {
			op, order = ">", "ASC"
		}
		var neighbor model.LibraryChapter
		err := tx.GetContext(ctx, &neighbor, `SELECT * FROM library_chapters WHERE item_id=? AND is_volume=0 AND lifecycle_state='active' AND (ord`+op+`? OR (ord=? AND id`+op+`?)) ORDER BY ord `+order+`,id `+order+` LIMIT 1`, itemID, current.Ord, current.Ord, current.Id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if direction {
			window.Next = &neighbor
		} else {
			window.Previous = &neighbor
		}
	}
	return window, tx.Commit()
}
