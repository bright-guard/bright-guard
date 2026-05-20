package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

type Users struct {
	Pool *pgxpool.Pool
}

func (s *Users) UpsertByGoogle(ctx context.Context, subject, email, name, avatar string) (*models.User, error) {
	const q = `
		insert into users (email, display_name, avatar_url, google_subject)
		values ($1, $2, $3, $4)
		on conflict (google_subject) do update
		set email = excluded.email,
		    display_name = excluded.display_name,
		    avatar_url = excluded.avatar_url,
		    updated_at = now()
		returning id, email, display_name, avatar_url, google_subject, created_at`
	u := &models.User{}
	err := s.Pool.QueryRow(ctx, q, email, name, avatar, subject).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.GoogleSubject, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

func (s *Users) ByID(ctx context.Context, id string) (*models.User, error) {
	const q = `select id, email, display_name, avatar_url, google_subject, created_at from users where id = $1`
	u := &models.User{}
	err := s.Pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.GoogleSubject, &u.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

var ErrNotFound = errors.New("not found")
