package gallery

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hildolfr/daz/internal/logger"
)

// HTMLGenerator generates static HTML galleries
type HTMLGenerator struct {
	store  *Store
	config *Config
}

// NewHTMLGenerator creates a new HTML generator
func NewHTMLGenerator(store *Store, config *Config) *HTMLGenerator {
	return &HTMLGenerator{
		store:  store,
		config: config,
	}
}

// GenerateAllGalleries generates HTML for all user galleries
func (g *HTMLGenerator) GenerateAllGalleries() error {
	// Get all users with galleries
	users, err := g.store.GetAllActiveUsers()
	if err != nil {
		return fmt.Errorf("failed to get active users: %w", err)
	}

	logger.Info("gallery", "Generating HTML for %d user galleries", len(users))

	for _, user := range users {
		if err := g.GenerateUserGallery(user.Username, user.Channel); err != nil {
			logger.Error("gallery", "Failed to generate gallery for %s: %v", user.Username, err)
			// Continue with other galleries
		}
	}

	// Generate index page
	if err := g.GenerateIndexPage(users); err != nil {
		logger.Error("gallery", "Failed to generate index page: %v", err)
	}

	logger.Info("gallery", "HTML generation completed")
	return nil
}

// GenerateUserGallery generates HTML for a specific user's gallery
func (g *HTMLGenerator) GenerateUserGallery(username, channel string) error {
	// Get user's images
	images, err := g.store.GetUserImages(username, channel)
	if err != nil {
		return fmt.Errorf("failed to get user images: %w", err)
	}

	// Get user stats
	stats, err := g.store.GetUserStats(username, channel)
	if err != nil {
		return fmt.Errorf("failed to get user stats: %w", err)
	}

	// Create output directory
	userDir := filepath.Join(g.config.HTMLOutputPath, username)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return fmt.Errorf("failed to create user directory: %w", err)
	}

	// Generate HTML
	htmlContent := g.generateGalleryHTML(username, channel, images, stats)

	// Write to file
	outputFile := filepath.Join(userDir, "index.html")
	if err := os.WriteFile(outputFile, []byte(htmlContent), 0644); err != nil {
		return fmt.Errorf("failed to write HTML file: %w", err)
	}

	logger.Debug("gallery", "Generated gallery for %s with %d images", username, len(images))
	return nil
}

// GenerateIndexPage generates the main index page listing all galleries
func (g *HTMLGenerator) GenerateIndexPage(users []struct{ Username, Channel string }) error {
	// Create output directory
	if err := os.MkdirAll(g.config.HTMLOutputPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	htmlContent := g.generateIndexHTML(users)

	// Write to file
	outputFile := filepath.Join(g.config.HTMLOutputPath, "index.html")
	if err := os.WriteFile(outputFile, []byte(htmlContent), 0644); err != nil {
		return fmt.Errorf("failed to write index HTML file: %w", err)
	}

	logger.Debug("gallery", "Generated index page with %d galleries", len(users))
	return nil
}

// generateGalleryHTML generates the HTML content for a user gallery
func (g *HTMLGenerator) generateGalleryHTML(username, channel string, images []*GalleryImage, stats *GalleryStats) string {
	tmplStr := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Username}}'s Gallery - Daz</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        
        body {
            font-family: 'Courier New', monospace;
            background: linear-gradient(135deg, #0a0e27 0%, #1a1f3a 100%);
            color: #00ff41;
            min-height: 100vh;
            padding: 20px;
        }
        
        .container {
            max-width: 1400px;
            margin: 0 auto;
        }
        
        .header {
            text-align: center;
            margin-bottom: 40px;
            padding: 30px;
            background: rgba(0, 255, 65, 0.1);
            border: 2px solid #00ff41;
            border-radius: 10px;
            box-shadow: 0 0 30px rgba(0, 255, 65, 0.3);
        }
        
        h1 {
            font-size: 3em;
            text-shadow: 0 0 20px rgba(0, 255, 65, 0.8);
            margin-bottom: 10px;
        }
        
        .stats {
            display: flex;
            justify-content: center;
            gap: 30px;
            margin-top: 20px;
            flex-wrap: wrap;
        }
        
        .stat {
            background: rgba(0, 0, 0, 0.5);
            padding: 10px 20px;
            border: 1px solid #00ff41;
            border-radius: 5px;
        }
        
        .locked-indicator {
            display: inline-block;
            background: #ff0041;
            color: white;
            padding: 5px 10px;
            border-radius: 5px;
            margin-left: 10px;
            animation: pulse 2s infinite;
        }
        
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.7; }
        }
        
        .gallery {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
            gap: 20px;
            margin-top: 40px;
        }
        
        .image-card {
            background: rgba(0, 0, 0, 0.7);
            border: 1px solid #00ff41;
            border-radius: 10px;
            overflow: hidden;
            transition: all 0.3s ease;
            position: relative;
        }
        
        .image-card:hover {
            transform: translateY(-5px);
            box-shadow: 0 10px 30px rgba(0, 255, 65, 0.4);
            border-color: #00ff88;
        }
        
        .image-container {
            width: 100%;
            height: 200px;
            background: #000;
            display: flex;
            align-items: center;
            justify-content: center;
            overflow: hidden;
        }
        
        .image-container img {
            max-width: 100%;
            max-height: 100%;
            object-fit: contain;
        }
        
        .image-info {
            padding: 15px;
            font-size: 0.9em;
        }
        
        .image-date {
            color: #00ff41;
            opacity: 0.7;
            font-size: 0.85em;
        }
        
        .image-title {
            margin-top: 5px;
            color: #ffffff;
            word-break: break-word;
        }
        
        .shared-from {
            margin-top: 5px;
            color: #ffaa00;
            font-size: 0.85em;
        }
        
        .copy-btn {
            position: absolute;
            top: 10px;
            right: 10px;
            background: rgba(0, 255, 65, 0.9);
            color: #000;
            border: none;
            padding: 5px 10px;
            border-radius: 5px;
            cursor: pointer;
            font-family: 'Courier New', monospace;
            font-weight: bold;
            opacity: 0;
            transition: opacity 0.3s;
        }
        
        .image-card:hover .copy-btn {
            opacity: 1;
        }
        
        .copy-btn:hover {
            background: #00ff88;
        }
        
        .footer {
            text-align: center;
            margin-top: 60px;
            padding: 20px;
            border-top: 1px solid #00ff41;
            opacity: 0.7;
        }
        
        .no-images {
            text-align: center;
            padding: 60px;
            font-size: 1.5em;
            opacity: 0.7;
        }
        
        @media (max-width: 768px) {
            .gallery {
                grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
                gap: 10px;
            }
            
            h1 {
                font-size: 2em;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>{{.Username}}'s Gallery{{if .Stats.IsLocked}}<span class="locked-indicator">LOCKED</span>{{end}}</h1>
            <div class="stats">
                <div class="stat">Active: {{.Stats.ActiveImages}}</div>
                <div class="stat">Total: {{.Stats.TotalImages}}</div>
                <div class="stat">Channel: {{.Channel}}</div>
                {{if .Stats.LastPostAt}}<div class="stat">Last Post: {{.LastPostFormatted}}</div>{{end}}
            </div>
        </div>
        
        {{if .Images}}
        <div class="gallery">
            {{range .Images}}
            <div class="image-card">
                <button class="copy-btn" onclick="copyURL('{{.URL}}')">Copy</button>
                <div class="image-container">
                    <img src="{{.URL}}" alt="Gallery image" loading="lazy" onerror="this.src='data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjAwIiBoZWlnaHQ9IjIwMCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cmVjdCB3aWR0aD0iMjAwIiBoZWlnaHQ9IjIwMCIgZmlsbD0iIzMzMyIvPjx0ZXh0IHg9IjUwJSIgeT0iNTAlIiBmb250LWZhbWlseT0iQXJpYWwiIGZvbnQtc2l6ZT0iMTQiIGZpbGw9IiM5OTkiIHRleHQtYW5jaG9yPSJtaWRkbGUiIGR5PSIuM2VtIj5JbWFnZSBVbmF2YWlsYWJsZTwvdGV4dD48L3N2Zz4='">
                </div>
                <div class="image-info">
                    <div class="image-date">{{.PostedAtFormatted}}</div>
                    {{if .ImageTitle}}<div class="image-title">{{.ImageTitle}}</div>{{end}}
                    {{if .OriginalPoster}}{{if ne .OriginalPoster .Username}}<div class="shared-from">Shared from: {{.OriginalPoster}}</div>{{end}}{{end}}
                </div>
            </div>
            {{end}}
        </div>
        {{else}}
        <div class="no-images">No images in gallery</div>
        {{end}}
        
        <div class="footer">
            <p>Generated by Daz Gallery System</p>
            <p>{{.GeneratedAt}}</p>
        </div>
    </div>
    
    <script>
        function copyURL(url) {
            navigator.clipboard.writeText(url).then(function() {
                // Show feedback
                event.target.textContent = 'Copied!';
                setTimeout(function() {
                    event.target.textContent = 'Copy';
                }, 2000);
            }).catch(function(err) {
                console.error('Failed to copy:', err);
            });
        }
    </script>
</body>
</html>`

	tmpl, err := template.New("gallery").Parse(tmplStr)
	if err != nil {
		logger.Error("gallery", "Failed to parse template: %v", err)
		return "<html><body>Error generating gallery</body></html>"
	}

	// Prepare template data
	data := struct {
		Username         string
		Channel          string
		Images           []*galleryImageDisplay
		Stats            *GalleryStats
		LastPostFormatted string
		GeneratedAt      string
	}{
		Username:    username,
		Channel:     channel,
		Stats:       stats,
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05 MST"),
	}

	// Format last post time
	if stats.LastPostAt != nil {
		data.LastPostFormatted = stats.LastPostAt.Format("Jan 2, 2006")
	}

	// Convert images for display
	data.Images = make([]*galleryImageDisplay, len(images))
	for i, img := range images {
		data.Images[i] = &galleryImageDisplay{
			URL:                img.URL,
			Username:           img.Username,
			PostedAtFormatted:  img.PostedAt.Format("Jan 2, 15:04"),
			ImageTitle:         getString(img.ImageTitle),
			OriginalPoster:     getString(img.OriginalPoster),
		}
	}

	// Execute template
	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		logger.Error("gallery", "Failed to execute template: %v", err)
		return "<html><body>Error generating gallery</body></html>"
	}

	return result.String()
}

// generateIndexHTML generates the HTML content for the index page
func (g *HTMLGenerator) generateIndexHTML(users []struct{ Username, Channel string }) string {
	tmplStr := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Daz Gallery Index</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        
        body {
            font-family: 'Courier New', monospace;
            background: linear-gradient(135deg, #0a0e27 0%, #1a1f3a 100%);
            color: #00ff41;
            min-height: 100vh;
            padding: 20px;
        }
        
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        
        h1 {
            text-align: center;
            font-size: 3em;
            text-shadow: 0 0 20px rgba(0, 255, 65, 0.8);
            margin-bottom: 40px;
        }
        
        .gallery-list {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
            gap: 20px;
        }
        
        .gallery-link {
            display: block;
            background: rgba(0, 0, 0, 0.7);
            border: 1px solid #00ff41;
            border-radius: 10px;
            padding: 20px;
            text-decoration: none;
            color: #00ff41;
            transition: all 0.3s ease;
            text-align: center;
        }
        
        .gallery-link:hover {
            transform: translateY(-5px);
            box-shadow: 0 10px 30px rgba(0, 255, 65, 0.4);
            border-color: #00ff88;
            background: rgba(0, 255, 65, 0.1);
        }
        
        .username {
            font-size: 1.5em;
            margin-bottom: 10px;
        }
        
        .channel {
            font-size: 0.9em;
            opacity: 0.7;
        }
        
        .footer {
            text-align: center;
            margin-top: 60px;
            padding: 20px;
            border-top: 1px solid #00ff41;
            opacity: 0.7;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Daz Gallery System</h1>
        
        <div class="gallery-list">
            {{range .Users}}
            <a href="./{{.Username}}/" class="gallery-link">
                <div class="username">{{.Username}}</div>
                <div class="channel">Channel: {{.Channel}}</div>
            </a>
            {{end}}
        </div>
        
        <div class="footer">
            <p>{{len .Users}} galleries available</p>
            <p>Generated: {{.GeneratedAt}}</p>
        </div>
    </div>
</body>
</html>`

	tmpl, err := template.New("index").Parse(tmplStr)
	if err != nil {
		logger.Error("gallery", "Failed to parse index template: %v", err)
		return "<html><body>Error generating index</body></html>"
	}

	// Prepare template data
	data := struct {
		Users       []struct{ Username, Channel string }
		GeneratedAt string
	}{
		Users:       users,
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05 MST"),
	}

	// Execute template
	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		logger.Error("gallery", "Failed to execute index template: %v", err)
		return "<html><body>Error generating index</body></html>"
	}

	return result.String()
}

// galleryImageDisplay is a display-friendly version of GalleryImage
type galleryImageDisplay struct {
	URL               string
	Username          string
	PostedAtFormatted string
	ImageTitle        string
	OriginalPoster    string
}

// getString safely converts a *string to string
func getString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}