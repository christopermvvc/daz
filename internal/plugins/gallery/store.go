package gallery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

// Store handles database operations for the gallery
type Store struct {
	sqlClient *framework.SQLClient
	name      string
	maxImages int
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
func NewStore(eventBus framework.EventBus, pluginName string, maxImages int) *Store {
	if maxImages <= 0 {
		maxImages = 25
	}
	return &Store{
		sqlClient: framework.NewSQLClient(eventBus, pluginName),
		name:      pluginName,
		maxImages: maxImages,
	}
}

// InitializeSchema ensures the database schema exists
func (s *Store) InitializeSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tablesReady, err := s.galleryTablesReady(ctx)
	if err != nil {
		return err
	}

	functionsReady, err := s.galleryFunctionsReady(ctx)
	if err != nil {
		return err
	}

	if !tablesReady || !functionsReady {
		logger.Warn(s.name, "Gallery schema incomplete. Applying migrations...")
		if err := s.applyGalleryMigrations(ctx); err != nil {
			return err
		}

		tablesReady, err = s.galleryTablesReady(ctx)
		if err != nil {
			return err
		}
		functionsReady, err = s.galleryFunctionsReady(ctx)
		if err != nil {
			return err
		}
	}

	if !tablesReady || !functionsReady {
		return fmt.Errorf("gallery schema not initialized")
	}

	logger.Debug(s.name, "Gallery database schema verified")
	return nil
}

func (s *Store) galleryTablesReady(ctx context.Context) (bool, error) {
	query := `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public'
		AND table_name IN ('daz_gallery_images', 'daz_gallery_locks', 'daz_gallery_stats')
	`

	var count int
	rows, err := s.sqlClient.QueryContext(ctx, query)
	if err != nil {
		return false, fmt.Errorf("failed to verify gallery tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return false, fmt.Errorf("failed to scan gallery table count: %w", err)
		}
	}

	return count >= 3, nil
}

func (s *Store) galleryFunctionsReady(ctx context.Context) (bool, error) {
	requiredFunctions := []string{
		"add_gallery_image",
		"get_images_for_health_check",
		"mark_image_for_health_check",
		"restore_dead_image",
		"update_dead_image_recovery",
	}

	for _, name := range requiredFunctions {
		exists, err := s.galleryFunctionExists(ctx, name)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}

	constraintReady, err := s.galleryUniqueConstraintReady(ctx)
	if err != nil {
		return false, err
	}
	if !constraintReady {
		return false, nil
	}

	definitionReady, err := s.galleryFunctionDefinitionReady(ctx)
	if err != nil {
		return false, err
	}
	if !definitionReady {
		return false, nil
	}

	return true, nil
}

func (s *Store) galleryFunctionExists(ctx context.Context, name string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1
			FROM pg_proc p
			JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'public'
			AND p.proname = $1
		)
	`

	var exists bool
	rows, err := s.sqlClient.QueryContext(ctx, query, name)
	if err != nil {
		return false, fmt.Errorf("failed to query function %s: %w", name, err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&exists); err != nil {
			return false, fmt.Errorf("failed to scan function %s: %w", name, err)
		}
	}

	return exists, nil
}

func (s *Store) galleryUniqueConstraintReady(ctx context.Context) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1
			FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			WHERE t.relname = 'daz_gallery_images'
			AND c.conname = 'daz_gallery_images_username_url_key'
		)
	`

	var exists bool
	rows, err := s.sqlClient.QueryContext(ctx, query)
	if err != nil {
		return false, fmt.Errorf("failed to query gallery constraint: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&exists); err != nil {
			return false, fmt.Errorf("failed to scan gallery constraint: %w", err)
		}
	}

	return exists, nil
}

func (s *Store) galleryFunctionDefinitionReady(ctx context.Context) (bool, error) {
	query := `
		SELECT pg_get_functiondef(p.oid)
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public'
		AND p.proname = 'add_gallery_image'
		AND p.pronargs = 5
		AND p.proargtypes::text = '1043 25 1043 25 23'
		LIMIT 1
	`

	var definition string
	rows, err := s.sqlClient.QueryContext(ctx, query)
	if err != nil {
		return false, fmt.Errorf("failed to query function definition: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&definition); err != nil {
			return false, fmt.Errorf("failed to scan function definition: %w", err)
		}
	}

	if definition == "" {
		return false, nil
	}

	return strings.Contains(definition, "ON CONFLICT (username, url)"), nil
}

func (s *Store) gallerySchemaReady(ctx context.Context) (bool, error) {
	tablesReady, err := s.galleryTablesReady(ctx)
	if err != nil {
		return false, err
	}
	functionsReady, err := s.galleryFunctionsReady(ctx)
	if err != nil {
		return false, err
	}

	return tablesReady && functionsReady, nil
}

func (s *Store) applyGalleryMigrations(ctx context.Context) error {
	migrationFiles := []string{
		"scripts/sql/027_gallery_system.sql",
		"scripts/sql/028_fix_gallery_race_condition.sql",
		"scripts/sql/029_graveyard_recovery_procedure.sql",
		"scripts/sql/030_fix_gallery_duplicates.sql",
	}

	for _, migrationFile := range migrationFiles {
		migrationSQL, err := s.loadMigrationSQL(migrationFile)
		if err != nil {
			return err
		}
		if migrationSQL == "" {
			continue
		}

		if _, err := s.sqlClient.ExecContext(ctx, migrationSQL); err != nil {
			ready, readyErr := s.gallerySchemaReady(ctx)
			if readyErr != nil {
				return fmt.Errorf("failed to apply migration %s: %w", migrationFile, err)
			}
			if !ready {
				return fmt.Errorf("failed to apply migration %s: %w", migrationFile, err)
			}

			logger.Warn(s.name, "Gallery migration %s failed but schema is present: %v", migrationFile, err)
		}
	}

	return nil
}

func (s *Store) loadMigrationSQL(relativePath string) (string, error) {
	path, err := s.resolveMigrationPath(relativePath)
	if err != nil {
		return "", err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read migration %s: %w", path, err)
	}

	return string(contents), nil
}

func (s *Store) resolveMigrationPath(relativePath string) (string, error) {
	candidates := []string{relativePath}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, relativePath))
	}
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		candidates = append(candidates,
			filepath.Join(execDir, relativePath),
			filepath.Join(execDir, "..", relativePath),
		)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate), nil
		}
	}

	return "", fmt.Errorf("migration file not found: %s", relativePath)
}

func (s *Store) ensureGallerySchema(ctx context.Context) error {
	migrationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return s.applyGalleryMigrations(migrationCtx)
}

func (s *Store) shouldRetryMigration(err error, functionName string) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "no unique or exclusion constraint matching the ON CONFLICT specification") {
		return true
	}
	if !strings.Contains(errMsg, "does not exist") {
		return false
	}
	if functionName == "" {
		return true
	}

	return strings.Contains(errMsg, functionName)
}

func (s *Store) queryWithSchemaRetry(ctx context.Context, query string, functionName string, args ...interface{}) (*framework.QueryRows, error) {
	rows, err := s.sqlClient.QueryContext(ctx, query, args...)
	if err == nil {
		return rows, nil
	}
	if !s.shouldRetryMigration(err, functionName) {
		return nil, err
	}
	if err := s.ensureGallerySchema(ctx); err != nil {
		return nil, err
	}

	return s.sqlClient.QueryContext(ctx, query, args...)
}

func (s *Store) execWithSchemaRetry(ctx context.Context, query string, functionName string, args ...interface{}) error {
	_, err := s.sqlClient.ExecContext(ctx, query, args...)
	if err == nil {
		return nil
	}
	if !s.shouldRetryMigration(err, functionName) {
		return err
	}
	if err := s.ensureGallerySchema(ctx); err != nil {
		return err
	}

	_, err = s.sqlClient.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) isGalleryLocked(ctx context.Context, username, channel string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM daz_gallery_locks
			WHERE username = $1 AND channel = $2 AND is_locked = true
		)
	`

	var locked bool
	rows, err := s.sqlClient.QueryContext(ctx, query, username, channel)
	if err != nil {
		return false, fmt.Errorf("failed to check gallery lock: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&locked); err != nil {
			return false, fmt.Errorf("failed to scan gallery lock: %w", err)
		}
	}

	return locked, nil
}

func (s *Store) isImageLocked(ctx context.Context, imageID int64) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1
			FROM daz_gallery_images gi
			JOIN daz_gallery_locks gl
				ON gl.username = gi.username AND gl.channel = gi.channel
			WHERE gi.id = $1 AND gl.is_locked = true
		)
	`

	var locked bool
	rows, err := s.sqlClient.QueryContext(ctx, query, imageID)
	if err != nil {
		return false, fmt.Errorf("failed to check image lock: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&locked); err != nil {
			return false, fmt.Errorf("failed to scan image lock: %w", err)
		}
	}

	return locked, nil
}

// AddImage adds an image to a user's gallery
func (s *Store) AddImage(username, url, channel string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	locked, err := s.isGalleryLocked(ctx, username, channel)
	if err != nil {
		return err
	}
	if locked {
		logger.Debug(s.name, "Gallery locked for user %s in channel %s; skipping add", username, channel)
		return nil
	}

	query := `SELECT add_gallery_image($1, $2, $3, NULL, $4)`

	var imageID int64
	rows, err := s.queryWithSchemaRetry(ctx, query, "add_gallery_image", username, url, channel, s.maxImages)
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

	var query string
	var rows *framework.QueryRows
	var err error

	if channel == "" {
		// Empty channel means get images from ALL channels for this user
		query = `
			SELECT gi.id, gi.username, gi.url, gi.channel, gi.posted_at, gi.is_active,
			       gi.failure_count, gi.first_failure_at, gi.last_check_at, gi.next_check_at,
			       gi.pruned_reason, gi.original_poster, gi.original_posted_at,
			       gi.most_recent_poster, gi.image_title
			FROM daz_gallery_images gi
			WHERE gi.username = $1
			  AND gi.is_active = true
			ORDER BY gi.posted_at DESC
		`

		rows, err = s.sqlClient.QueryContext(ctx, query, username)
	} else {
		// Specific channel
		query = `
			SELECT id, username, url, channel, posted_at, is_active,
				   failure_count, first_failure_at, last_check_at, next_check_at,
				   pruned_reason, original_poster, original_posted_at, 
				   most_recent_poster, image_title
			FROM daz_gallery_images
			WHERE username = $1 AND channel = $2 AND is_active = true
			ORDER BY posted_at DESC
		`
		rows, err = s.sqlClient.QueryContext(ctx, query, username, channel)
	}
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
	var statsQuery string
	var rows *framework.QueryRows
	var err error

	if channel == "" {
		// Aggregate stats across all channels for this user
		statsQuery = `
			SELECT SUM(total_images), SUM(active_images), SUM(COALESCE(dead_images, 0)), 
				   SUM(COALESCE(images_shared, 0)), MAX(last_post_at), SUM(gallery_views)
			FROM daz_gallery_stats
			WHERE username = $1
		`
		rows, err = s.sqlClient.QueryContext(ctx, statsQuery, username)
	} else {
		// Get stats for specific channel
		statsQuery = `
			SELECT total_images, active_images, COALESCE(dead_images, 0), 
				   COALESCE(images_shared, 0), last_post_at, gallery_views
			FROM daz_gallery_stats
			WHERE username = $1 AND channel = $2
		`
		rows, err = s.sqlClient.QueryContext(ctx, statsQuery, username, channel)
	}

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

	// Get lock status (if any channel is locked, consider user locked)
	var lockQuery string
	var rows2 *framework.QueryRows

	if channel == "" {
		// Check if ANY of the user's galleries are locked
		lockQuery = `
			SELECT EXISTS(
				SELECT 1 FROM daz_gallery_locks
				WHERE username = $1 AND is_locked = true
			)
		`
		rows2, err = s.sqlClient.QueryContext(ctx, lockQuery, username)
	} else {
		lockQuery = `
			SELECT is_locked FROM daz_gallery_locks
			WHERE username = $1 AND channel = $2
		`
		rows2, err = s.sqlClient.QueryContext(ctx, lockQuery, username, channel)
	}

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

	rows, err := s.queryWithSchemaRetry(ctx, query, "get_images_for_health_check", limit)
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

	locked, err := s.isImageLocked(ctx, imageID)
	if err != nil {
		return err
	}
	if locked {
		logger.Debug(s.name, "Gallery locked for image %d; skipping health update", imageID)
		return nil
	}

	query := `SELECT mark_image_for_health_check($1, $2, $3)`

	err = s.execWithSchemaRetry(ctx, query, "mark_image_for_health_check", imageID, failed, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to mark health check: %w", err)
	}

	return nil
}

// RestoreDeadImage attempts to restore a dead image if space is available
func (s *Store) RestoreDeadImage(imageID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `SELECT restore_dead_image($1, $2)`

	var restored bool
	rows, err := s.queryWithSchemaRetry(ctx, query, "restore_dead_image", imageID, s.maxImages)
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

// GetAllActiveUsers gets all users with galleries (combined across all channels)
func (s *Store) GetAllActiveUsers() ([]struct{ Username, Channel string }, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get unique usernames only, ignoring channel
	query := `
		SELECT DISTINCT username 
		FROM daz_gallery_images 
		WHERE is_active = true
		ORDER BY username
	`

	rows, err := s.sqlClient.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get active users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []struct{ Username, Channel string }
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		// Return empty channel to indicate "all channels"
		users = append(users, struct{ Username, Channel string }{
			Username: username,
			Channel:  "", // Empty means all channels
		})
	}

	return users, nil
}

// GetPrunedImages retrieves dead/pruned images for graveyard display
func (s *Store) GetPrunedImages(limit int) ([]*GalleryImage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get pruned images from last 6 months, ordered by most recent failures
	query := `
		SELECT id, username, url, channel, posted_at, is_active,
		       failure_count, first_failure_at, last_check_at, next_check_at,
		       pruned_reason, original_poster, original_posted_at, 
		       most_recent_poster, image_title
		FROM daz_gallery_images
		WHERE is_active = false 
		  AND pruned_reason IS NOT NULL
		  AND (last_check_at > NOW() - INTERVAL '6 months' OR last_check_at IS NULL)
		ORDER BY COALESCE(last_check_at, posted_at) DESC
		LIMIT $1
	`

	rows, err := s.sqlClient.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pruned images: %w", err)
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
			return nil, fmt.Errorf("failed to scan pruned image: %w", err)
		}
		images = append(images, img)
	}

	return images, nil
}

// GetDeadImagesForRecovery retrieves dead images that should be rechecked for recovery
func (s *Store) GetDeadImagesForRecovery(limit int) ([]*GalleryImage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get dead images that are due for recheck using exponential backoff
	// Recheck intervals: 3h, 6h, 12h, 24h, 48h (max)
	query := `
		SELECT id, username, url, channel, posted_at, is_active,
		       failure_count, first_failure_at, last_check_at, next_check_at,
		       pruned_reason, original_poster, original_posted_at, 
		       most_recent_poster, image_title
		FROM daz_gallery_images
		WHERE is_active = false 
		  AND pruned_reason IS NOT NULL
		  AND (next_check_at IS NULL OR next_check_at <= NOW())
		  AND (last_check_at IS NULL OR last_check_at > NOW() - INTERVAL '7 days')
		ORDER BY COALESCE(next_check_at, last_check_at, posted_at) ASC
		LIMIT $1
	`

	rows, err := s.sqlClient.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query dead images for recovery: %w", err)
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
			return nil, fmt.Errorf("failed to scan dead image: %w", err)
		}
		images = append(images, img)
	}

	return images, nil
}

// RecoverDeadImage attempts to recover a dead image that is now accessible
func (s *Store) RecoverDeadImage(imageID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First check if image is actually dead
	var isActive bool
	checkQuery := `SELECT is_active FROM daz_gallery_images WHERE id = $1`
	rows, err := s.sqlClient.QueryContext(ctx, checkQuery, imageID)
	if err != nil {
		return false, fmt.Errorf("failed to check image status: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&isActive); err != nil {
			return false, fmt.Errorf("failed to scan active status: %w", err)
		}
	}

	if isActive {
		// Image is already active, nothing to recover
		return false, nil
	}

	// Attempt to recover the image using the stored function
	recoverQuery := `SELECT restore_dead_image($1, $2)`
	var recovered bool
	rows2, err := s.queryWithSchemaRetry(ctx, recoverQuery, "restore_dead_image", imageID, s.maxImages)
	if err != nil {
		return false, fmt.Errorf("failed to recover image: %w", err)
	}
	defer func() { _ = rows2.Close() }()

	if rows2.Next() {
		if err := rows2.Scan(&recovered); err != nil {
			return false, fmt.Errorf("failed to scan recovery status: %w", err)
		}
	}

	if recovered {
		logger.Info("gallery", "Successfully recovered dead image %d", imageID)
	} else {
		// Update next check time using stored procedure for exponential backoff
		updateQuery := `SELECT update_dead_image_recovery($1)`
		if err := s.execWithSchemaRetry(ctx, updateQuery, "update_dead_image_recovery", imageID); err != nil {
			logger.Error("gallery", "Failed to update next check time for image %d: %v", imageID, err)
		}
	}

	return recovered, nil
}
