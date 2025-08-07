package gallery

import (
	"context"
	"fmt"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

// Store handles database operations for the gallery
type Store struct {
	sqlClient *framework.SQLClient
	name      string
}

// GalleryImage represents an image in the gallery
type GalleryImage struct {
	ID               int64
	Username         string
	URL              string
	Channel          string
	PostedAt         time.Time
	IsActive         bool
	FailureCount     int
	FirstFailureAt   *time.Time
	LastCheckAt      *time.Time
	NextCheckAt      *time.Time
	PrunedReason     *string
	OriginalPoster   *string
	OriginalPostedAt *time.Time
	MostRecentPoster *string
	ImageTitle       *string
}

// GalleryStats represents gallery statistics for a user
type GalleryStats struct {
	Username     string
	Channel      string
	TotalImages  int
	ActiveImages int
	DeadImages   int
	ImagesShared int
	LastPostAt   *time.Time
	GalleryViews int
	IsLocked     bool
}

// NewStore creates a new store instance
func NewStore(eventBus framework.EventBus, pluginName string) *Store {
	return &Store{
		sqlClient: framework.NewSQLClient(eventBus, pluginName),
		name:      pluginName,
	}
}

// InitializeSchema ensures the database schema exists
func (s *Store) InitializeSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Note: Schema is created by SQL migration file 027_gallery_system.sql
	// Just verify the tables exist
	query := `
		SELECT COUNT(*) FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_name IN ('daz_gallery_images', 'daz_gallery_locks', 'daz_gallery_stats')
	`

	var count int
	rows, err := s.sqlClient.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to verify schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return fmt.Errorf("failed to scan count: %w", err)
		}
	}

	if count < 3 {
		logger.Warn(s.name, "Gallery tables not found. Please run SQL migration 027_gallery_system.sql")
		return fmt.Errorf("gallery tables not initialized")
	}

	logger.Debug(s.name, "Gallery database schema verified")
	return nil
}

// AddImage adds an image to a user's gallery
func (s *Store) AddImage(username, url, channel string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the stored function to handle limit enforcement
	query := `SELECT add_gallery_image($1, $2, $3, NULL)`

	var imageID int64
	rows, err := s.sqlClient.QueryContext(ctx, query, username, url, channel)
	if err != nil {
		return fmt.Errorf("failed to add image: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&imageID); err != nil {
			return fmt.Errorf("failed to scan image ID: %w", err)
		}
	}

	logger.Debug(s.name, "Added image %d for user %s", imageID, username)
	return nil
}

// GetUserImages retrieves all active images for a user
func (s *Store) GetUserImages(username, channel string) ([]*GalleryImage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		SELECT id, username, url, channel, posted_at, is_active,
			   failure_count, first_failure_at, last_check_at, next_check_at,
			   pruned_reason, original_poster, original_posted_at, 
			   most_recent_poster, image_title
		FROM daz_gallery_images
		WHERE username = $1 AND channel = $2 AND is_active = true
		ORDER BY posted_at DESC
	`

	rows, err := s.sqlClient.QueryContext(ctx, query, username, channel)
	if err != nil {
		return nil, fmt.Errorf("failed to query images: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var images []*GalleryImage
	for rows.Next() {
		img := &GalleryImage{}
		err := rows.Scan(
			&img.ID, &img.Username, &img.URL, &img.Channel, &img.PostedAt,
			&img.IsActive, &img.FailureCount, &img.FirstFailureAt,
			&img.LastCheckAt, &img.NextCheckAt, &img.PrunedReason,
			&img.OriginalPoster, &img.OriginalPostedAt, &img.MostRecentPoster,
			&img.ImageTitle,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan image: %w", err)
		}
		images = append(images, img)
	}

	return images, nil
}

// GetUserStats retrieves gallery statistics for a user
func (s *Store) GetUserStats(username, channel string) (*GalleryStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats := &GalleryStats{
		Username: username,
		Channel:  channel,
	}

	// Get stats from daz_gallery_stats table
	statsQuery := `
		SELECT total_images, active_images, COALESCE(dead_images, 0), 
			   COALESCE(images_shared, 0), last_post_at, gallery_views
		FROM daz_gallery_stats
		WHERE username = $1 AND channel = $2
	`

	rows, err := s.sqlClient.QueryContext(ctx, statsQuery, username, channel)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(
			&stats.TotalImages, &stats.ActiveImages, &stats.DeadImages,
			&stats.ImagesShared, &stats.LastPostAt, &stats.GalleryViews,
		); err != nil {
			return nil, fmt.Errorf("failed to scan stats: %w", err)
		}
	}

	// Get lock status
	lockQuery := `
		SELECT is_locked FROM daz_gallery_locks
		WHERE username = $1 AND channel = $2
	`

	rows2, err := s.sqlClient.QueryContext(ctx, lockQuery, username, channel)
	if err != nil {
		return nil, fmt.Errorf("failed to get lock status: %w", err)
	}
	defer func() { _ = rows2.Close() }()

	if rows2.Next() {
		if err := rows2.Scan(&stats.IsLocked); err != nil {
			return nil, fmt.Errorf("failed to scan lock status: %w", err)
		}
	}

	return stats, nil
}

// LockGallery locks a user's gallery
func (s *Store) LockGallery(username, channel string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO daz_gallery_locks (username, channel, is_locked, locked_at, locked_by)
		VALUES ($1, $2, true, NOW(), $3)
		ON CONFLICT (username, channel)
		DO UPDATE SET is_locked = true, locked_at = NOW(), locked_by = $3, updated_at = NOW()
	`

	_, err := s.sqlClient.ExecContext(ctx, query, username, channel, username)
	if err != nil {
		return fmt.Errorf("failed to lock gallery: %w", err)
	}

	logger.Info(s.name, "Gallery locked for user %s in channel %s", username, channel)
	return nil
}

// UnlockGallery unlocks a user's gallery
func (s *Store) UnlockGallery(username, channel string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		UPDATE daz_gallery_locks 
		SET is_locked = false, updated_at = NOW()
		WHERE username = $1 AND channel = $2
	`

	_, err := s.sqlClient.ExecContext(ctx, query, username, channel)
	if err != nil {
		return fmt.Errorf("failed to unlock gallery: %w", err)
	}

	logger.Info(s.name, "Gallery unlocked for user %s in channel %s", username, channel)
	return nil
}

// GetImagesForHealthCheck retrieves images that need health checking
func (s *Store) GetImagesForHealthCheck(limit int) ([]*GalleryImage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `SELECT * FROM get_images_for_health_check($1)`

	rows, err := s.sqlClient.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get images for health check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var images []*GalleryImage
	for rows.Next() {
		img := &GalleryImage{}
		err := rows.Scan(&img.ID, &img.URL, &img.FailureCount)
		if err != nil {
			return nil, fmt.Errorf("failed to scan image: %w", err)
		}
		images = append(images, img)
	}

	return images, nil
}

// MarkImageHealthCheck updates the health status of an image
func (s *Store) MarkImageHealthCheck(imageID int64, failed bool, errorMsg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `SELECT mark_image_for_health_check($1, $2, $3)`

	_, err := s.sqlClient.ExecContext(ctx, query, imageID, failed, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to mark health check: %w", err)
	}

	return nil
}

// RestoreDeadImage attempts to restore a dead image if space is available
func (s *Store) RestoreDeadImage(imageID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `SELECT restore_dead_image($1)`

	var restored bool
	rows, err := s.sqlClient.QueryContext(ctx, query, imageID)
	if err != nil {
		return false, fmt.Errorf("failed to restore image: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&restored); err != nil {
			return false, fmt.Errorf("failed to scan restored status: %w", err)
		}
	}

	return restored, nil
}

// GetAllActiveUsers gets all users with galleries
func (s *Store) GetAllActiveUsers() ([]struct{ Username, Channel string }, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
		SELECT DISTINCT username, channel 
		FROM daz_gallery_images 
		WHERE is_active = true
		ORDER BY username, channel
	`

	rows, err := s.sqlClient.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get active users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []struct{ Username, Channel string }
	for rows.Next() {
		var user struct{ Username, Channel string }
		if err := rows.Scan(&user.Username, &user.Channel); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, user)
	}

	return users, nil
}
