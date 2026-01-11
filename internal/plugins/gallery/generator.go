package gallery

import (
	"context"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/logger"
)

// HTMLGenerator generates static HTML galleries
type HTMLGenerator struct {
	store  *Store
	config *Config
	mu     sync.Mutex // Protects git operations from concurrent access
}

// UserGalleryData holds gallery data for a single user
type UserGalleryData struct {
	Username string
	Channel  string
	Images   []*GalleryImage
	Stats    *GalleryStats
}

// NewHTMLGenerator creates a new HTML generator
func NewHTMLGenerator(store *Store, config *Config) *HTMLGenerator {
	return &HTMLGenerator{
		store:  store,
		config: config,
	}
}

// GenerateAllGalleries generates a single shared HTML gallery for all users
func (g *HTMLGenerator) GenerateAllGalleries() error {
	// Prevent concurrent generation to avoid git conflicts
	g.mu.Lock()
	defer g.mu.Unlock()

	// Get all users with galleries
	users, err := g.store.GetAllActiveUsers()
	if err != nil {
		return fmt.Errorf("failed to get active users: %w", err)
	}

	logger.Info("gallery", "Generating shared gallery HTML for %d users", len(users))

	// Generate the single shared gallery page
	if err := g.GenerateSharedGallery(users); err != nil {
		logger.Error("gallery", "Failed to generate shared gallery: %v", err)
		return err
	}

	// Push to GitHub Pages (non-fatal if it fails)
	if err := g.pushToGitHub(); err != nil {
		logger.Warn("gallery", "Failed to push to GitHub Pages (continuing anyway): %v", err)
		// Don't return error - HTML was still generated successfully
	} else {
		logger.Info("gallery", "HTML generation and GitHub push completed")
	}

	logger.Info("gallery", "HTML generation completed")
	return nil
}

// GenerateSharedGallery generates a single shared gallery page with all users' images
func (g *HTMLGenerator) GenerateSharedGallery(users []struct{ Username, Channel string }) error {
	if !g.isSafeOutputPath() {
		return fmt.Errorf("unsafe html output path: %s", g.config.HTMLOutputPath)
	}
	if err := os.MkdirAll(g.config.HTMLOutputPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	if err := g.ensureMarkerFile(); err != nil {
		logger.Warn("gallery", "Failed to write gallery marker file: %v", err)
	}

	// Collect all user galleries
	var allGalleries []UserGalleryData
	var totalImages int

	for _, user := range users {
		// Skip locked galleries
		stats, err := g.store.GetUserStats(user.Username, user.Channel)
		if err != nil {
			logger.Error("gallery", "Failed to get stats for %s: %v", user.Username, err)
			continue
		}

		if user.Channel != "" && stats.IsLocked {
			continue // Skip locked galleries from public view
		}

		images, err := g.store.GetUserImages(user.Username, user.Channel)
		if err != nil {
			logger.Error("gallery", "Failed to get images for %s: %v", user.Username, err)
			continue
		}

		if len(images) > 0 {
			allGalleries = append(allGalleries, UserGalleryData{
				Username: user.Username,
				Channel:  user.Channel,
				Images:   images,
				Stats:    stats,
			})
			totalImages += len(images)
		}
	}

	// Sort galleries by most recent activity
	sort.Slice(allGalleries, func(i, j int) bool {
		if len(allGalleries[i].Images) == 0 || len(allGalleries[j].Images) == 0 {
			return false
		}
		return allGalleries[i].Images[0].PostedAt.After(allGalleries[j].Images[0].PostedAt)
	})

	// Get pruned images for graveyard section
	prunedImages, err := g.store.GetPrunedImages(100) // Show up to 100 dead images
	if err != nil {
		logger.Error("gallery", "Failed to get pruned images: %v", err)
		prunedImages = []*GalleryImage{} // Continue with empty graveyard
	}

	// Generate the shared gallery HTML
	htmlContent := g.generateSharedGalleryHTML(allGalleries, totalImages, prunedImages)

	// Write to file
	outputFile := filepath.Join(g.config.HTMLOutputPath, "index.html")
	if err := os.WriteFile(outputFile, []byte(htmlContent), 0644); err != nil {
		return fmt.Errorf("failed to write shared gallery HTML: %w", err)
	}

	logger.Debug("gallery", "Generated shared gallery with %d users and %d total images", len(allGalleries), totalImages)
	return nil
}

// galleryImageDisplay is a display-friendly version of GalleryImage
type galleryImageDisplay struct {
	URL               string
	Username          string
	PostedAtFormatted string
	ImageTitle        string
	OriginalPoster    string
	IsVideo           bool   // True for webm files
	MediaType         string // "video" or "image"
}

// PrunedImageDisplay is a display-friendly version of pruned/dead images
type PrunedImageDisplay struct {
	Username     string
	URL          string
	PrunedReason string
	DeadSince    string
	IsGravestone bool // Dead for over 48 hours
}

// getString safely converts a *string to string
func getString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// getMediaType determines if URL is a video or image
func getMediaType(url string) string {
	lowerURL := strings.ToLower(url)
	if strings.HasSuffix(lowerURL, ".webm") {
		return "video"
	}
	return "image"
}

// generateSharedGalleryHTML generates HTML for the shared gallery page
func (g *HTMLGenerator) generateSharedGalleryHTML(galleries []UserGalleryData, totalImages int, prunedImages []*GalleryImage) string {
	tmplStr := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Daz Gallery - All Users</title>
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
            font-size: 1.2em;
            opacity: 0.9;
        }
        
        .user-section {
            margin-bottom: 60px;
            padding: 20px;
            background: rgba(0, 0, 0, 0.3);
            border-radius: 10px;
            border: 1px solid rgba(0, 255, 65, 0.3);
        }
        
        .user-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 20px;
            padding-bottom: 10px;
            border-bottom: 2px solid #00ff41;
        }
        
        .user-title {
            font-size: 1.8em;
            color: #00ff88;
        }
        
        .user-stats {
            opacity: 0.8;
        }
        
        .gallery {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
            gap: 20px;
        }
        
        .image-card {
            background: rgba(0, 0, 0, 0.5);
            border: 1px solid #00ff41;
            border-radius: 5px;
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
            position: relative;
            width: 100%;
            padding-bottom: 75%;
            background: #000;
            overflow: hidden;
            cursor: zoom-in;
        }
        
        .image-container img,
        .image-container video {
            position: absolute;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
            object-fit: cover;
            transition: transform 0.3s ease;
        }
        
        .image-container video {
            background: #000;
        }
        
        .image-card:hover img,
        .image-card:hover video {
            transform: scale(1.05);
        }
        
        /* Modal Styles */
        .modal {
            display: none;
            position: fixed;
            z-index: 9999;
            left: 0;
            top: 0;
            width: 100%;
            height: 100%;
            background: rgba(0, 0, 0, 0.95);
            animation: fadeIn 0.3s ease;
        }
        
        .modal.active {
            display: flex;
            align-items: center;
            justify-content: center;
        }
        
        @keyframes fadeIn {
            from { opacity: 0; }
            to { opacity: 1; }
        }
        
        @keyframes slideIn {
            from { 
                transform: scale(0.8);
                opacity: 0;
            }
            to { 
                transform: scale(1);
                opacity: 1;
            }
        }
        
        .modal-content {
            position: relative;
            max-width: 90vw;
            max-height: 90vh;
            animation: slideIn 0.3s ease;
        }
        
        .modal-media {
            max-width: 90vw;
            max-height: 90vh;
            object-fit: contain;
            box-shadow: 0 0 50px rgba(0, 255, 65, 0.5);
            border: 2px solid rgba(0, 255, 65, 0.3);
            border-radius: 5px;
        }
        
        .modal-close {
            position: absolute;
            top: 20px;
            right: 40px;
            color: #00ff41;
            font-size: 40px;
            font-weight: bold;
            cursor: pointer;
            z-index: 10000;
            text-shadow: 0 0 10px rgba(0, 255, 65, 0.8);
            transition: all 0.3s ease;
        }
        
        .modal-close:hover {
            color: #00ff88;
            transform: scale(1.2) rotate(90deg);
        }
        
        .modal-nav {
            position: absolute;
            top: 50%;
            transform: translateY(-50%);
            font-size: 60px;
            color: #00ff41;
            cursor: pointer;
            padding: 10px;
            user-select: none;
            text-shadow: 0 0 20px rgba(0, 255, 65, 0.8);
            transition: all 0.3s ease;
            z-index: 10000;
        }
        
        .modal-nav:hover {
            color: #00ff88;
            transform: translateY(-50%) scale(1.2);
        }
        
        .modal-prev {
            left: 20px;
        }
        
        .modal-next {
            right: 20px;
        }
        
        .modal-info {
            position: absolute;
            bottom: 20px;
            left: 50%;
            transform: translateX(-50%);
            background: rgba(0, 0, 0, 0.8);
            color: #00ff41;
            padding: 10px 20px;
            border-radius: 5px;
            border: 1px solid rgba(0, 255, 65, 0.5);
            font-family: 'Courier New', monospace;
            text-align: center;
            max-width: 80%;
        }
        
        .modal-info .username {
            color: #00ff88;
            font-weight: bold;
        }
        
        .zoom-indicator {
            position: absolute;
            top: 10px;
            right: 10px;
            background: rgba(0, 255, 65, 0.2);
            color: #00ff41;
            padding: 5px 10px;
            border-radius: 3px;
            font-size: 0.8em;
            opacity: 0;
            transition: opacity 0.3s ease;
            pointer-events: none;
        }
        
        .image-card:hover .zoom-indicator {
            opacity: 1;
        }
        
        .image-info {
            padding: 10px;
            font-size: 0.9em;
        }
        
        .image-date {
            color: #00ff88;
            opacity: 0.8;
        }
        
        .copy-btn {
            position: absolute;
            top: 10px;
            right: 10px;
            background: rgba(0, 255, 65, 0.9);
            color: #000;
            border: none;
            padding: 5px 10px;
            border-radius: 3px;
            cursor: pointer;
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
            padding-top: 20px;
            border-top: 1px solid rgba(0, 255, 65, 0.3);
            opacity: 0.7;
        }
        
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }
        
        .updated {
            animation: pulse 2s infinite;
        }
        
        /* Graveyard Section Styles */
        .graveyard-section {
            margin-top: 100px;
            padding: 30px;
            background: rgba(255, 0, 0, 0.05);
            border: 2px solid #ff3333;
            border-radius: 10px;
        }
        
        .graveyard-header {
            text-align: center;
            margin-bottom: 30px;
            padding-bottom: 20px;
            border-bottom: 2px solid #ff3333;
        }
        
        .graveyard-title {
            font-size: 2.5em;
            color: #ff3333;
            text-shadow: 0 0 20px rgba(255, 51, 51, 0.8);
            margin-bottom: 10px;
        }
        
        .graveyard-stats {
            color: #ff6666;
            opacity: 0.9;
        }
        
        .graveyard-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
            gap: 15px;
        }
        
        .dead-image {
            background: rgba(0, 0, 0, 0.7);
            border: 1px solid #ff3333;
            border-radius: 5px;
            padding: 15px;
            position: relative;
            opacity: 0.8;
        }
        
        .dead-image:hover {
            opacity: 1;
            box-shadow: 0 0 10px rgba(255, 51, 51, 0.4);
        }
        
        .dead-url {
            color: #ff6666;
            word-break: break-all;
            font-size: 0.9em;
            text-decoration: line-through;
        }
        
        .dead-user {
            color: #ff9999;
            font-weight: bold;
            margin-bottom: 5px;
        }
        
        .dead-reason {
            color: #ffaaaa;
            font-style: italic;
            font-size: 0.85em;
            margin-top: 5px;
        }
        
        .dead-date {
            color: #ff8888;
            font-size: 0.8em;
            margin-top: 5px;
        }
        
        .gravestone {
            position: absolute;
            top: 10px;
            right: 10px;
            font-size: 1.5em;
        }
        
        /* Collapsible graveyard controls */
        .graveyard-toggle {
            text-align: center;
            margin: 20px 0;
        }
        
        .toggle-btn {
            background: rgba(255, 51, 51, 0.2);
            color: #ff6666;
            border: 2px solid #ff3333;
            padding: 15px 30px;
            font-size: 1.2em;
            border-radius: 5px;
            cursor: pointer;
            transition: all 0.3s ease;
            font-family: 'Courier New', monospace;
        }
        
        .toggle-btn:hover {
            background: rgba(255, 51, 51, 0.3);
            color: #ff9999;
            box-shadow: 0 0 20px rgba(255, 51, 51, 0.5);
        }
        
        .graveyard-content {
            display: none;
            animation: fadeIn 0.5s ease;
        }
        
        .graveyard-content.expanded {
            display: block;
        }
        
        @keyframes fadeIn {
            from { opacity: 0; transform: translateY(-20px); }
            to { opacity: 1; transform: translateY(0); }
        }
        
        .dead-count {
            color: #ff9999;
            font-weight: bold;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🤖 DAZ GALLERY 🤖</h1>
            <div class="stats">
                <span>{{.TotalUsers}} Users</span> • 
                <span>{{.TotalImages}} Images</span> • 
                <span class="updated">Updated: {{.UpdateTime}}</span>
            </div>
        </div>
        
        {{range .Galleries}}
        <div class="user-section" id="{{.Username}}">
            <div class="user-header">
                <div class="user-title">{{.Username}}'s Gallery</div>
                <div class="user-stats">{{len .Images}} images</div>
            </div>
            <div class="gallery">
                {{range .Images}}
                <div class="image-card" data-url="{{.URL}}" data-type="{{.MediaType}}" data-user="{{.Username}}" data-date="{{.PostedAtFormatted}}">
                    <button class="copy-btn" onclick="copyURL(event, '{{.URL | js}}')">Copy</button>
                    <span class="zoom-indicator">🔍 Click to expand</span>
                    <div class="image-container" onclick="openModal(event, this.parentElement)">
                        {{if eq .MediaType "video"}}
                        <video src="{{.URL}}" autoplay loop muted playsinline>
                            Your browser does not support the video tag.
                        </video>
                        {{else}}
                        <img src="{{.URL}}" alt="Gallery image" loading="lazy" onerror="this.src='data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjAwIiBoZWlnaHQ9IjIwMCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cmVjdCB3aWR0aD0iMjAwIiBoZWlnaHQ9IjIwMCIgZmlsbD0iIzMzMyIvPjx0ZXh0IHg9IjUwJSIgeT0iNTAlIiBmb250LWZhbWlseT0iQXJpYWwiIGZvbnQtc2l6ZT0iMTQiIGZpbGw9IiM5OTkiIHRleHQtYW5jaG9yPSJtaWRkbGUiIGR5PSIuM2VtIj5JbWFnZSBVbmF2YWlsYWJsZTwvdGV4dD48L3N2Zz4='">
                        {{end}}
                    </div>
                    <div class="image-info">
                        <div class="image-date">{{.PostedAtFormatted}}</div>
                        {{if .ImageTitle}}<div>{{.ImageTitle}}</div>{{end}}
                    </div>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
        
        {{if .PrunedImages}}
        <div class="graveyard-section">
            <div class="graveyard-header">
                <div class="graveyard-title">🪦 THE GRAVEYARD 🪦</div>
                <div class="graveyard-toggle">
                    <button class="toggle-btn" onclick="toggleGraveyard()">
                        <span id="toggle-text">☠️ Show <span class="dead-count">{{len .PrunedImages}}</span> Dead Images ☠️</span>
                    </button>
                </div>
            </div>
            <div id="graveyard-content" class="graveyard-content">
                <div class="graveyard-stats">
                    <span>{{len .PrunedImages}} Dead Images</span> • 
                    <span>RIP</span>
                </div>
                <div class="graveyard-grid">
                    {{range .PrunedImages}}
                    <div class="dead-image">
                        {{if .IsGravestone}}<span class="gravestone">⚰️</span>{{end}}
                        <div class="dead-user">{{.Username}}</div>
                        <div class="dead-url">{{.URL}}</div>
                        {{if .PrunedReason}}<div class="dead-reason">{{.PrunedReason}}</div>{{end}}
                        <div class="dead-date">Died: {{.DeadSince}}</div>
                    </div>
                    {{end}}
                </div>
            </div>
        </div>
        {{end}}
        
        <div class="footer">
            <p>Powered by Daz Bot • Gallery updates every 5 minutes</p>
        </div>
    </div>
    
    <!-- Modal for full-size viewing -->
    <div id="imageModal" class="modal" onclick="handleModalClick(event)">
        <span class="modal-close" onclick="closeModal()">&times;</span>
        <span class="modal-nav modal-prev" onclick="navigateModal(-1, event)">‹</span>
        <span class="modal-nav modal-next" onclick="navigateModal(1, event)">›</span>
        <div class="modal-content">
            <div id="modalMedia"></div>
            <div class="modal-info" id="modalInfo"></div>
        </div>
    </div>
    
    <script>
        function copyURL(event, url) {
            navigator.clipboard.writeText(url).then(() => {
                const btn = event.target;
                const originalText = btn.textContent;
                btn.textContent = 'Copied!';
                btn.style.background = '#00ff88';
                setTimeout(() => {
                    btn.textContent = originalText;
                    btn.style.background = '';
                }, 2000);
            });
        }
        
        function toggleGraveyard() {
            const content = document.getElementById('graveyard-content');
            const toggleText = document.getElementById('toggle-text');
            const deadCount = {{len .PrunedImages}};
            
            if (content.classList.contains('expanded')) {
                content.classList.remove('expanded');
                toggleText.innerHTML = '☠️ Show <span class="dead-count">' + deadCount + '</span> Dead Images ☠️';
            } else {
                content.classList.add('expanded');
                toggleText.innerHTML = '☠️ Hide Graveyard ☠️';
            }
        }
        
        // Modal functionality
        let currentCards = [];
        let currentIndex = 0;
        
        function openModal(event, card) {
            event.stopPropagation();
            const modal = document.getElementById('imageModal');
            const allCards = Array.from(card.parentElement.parentElement.querySelectorAll('.image-card'));
            currentCards = allCards;
            currentIndex = allCards.indexOf(card);
            
            showModalContent(card);
            modal.classList.add('active');
            document.body.style.overflow = 'hidden';
        }
        
        function showModalContent(card) {
            const url = card.dataset.url;
            const type = card.dataset.type;
            const user = card.dataset.user;
            const date = card.dataset.date;
            
            const modalMedia = document.getElementById('modalMedia');
            const modalInfo = document.getElementById('modalInfo');
            
            if (type === 'video') {
                modalMedia.innerHTML = '<video class="modal-media" src="' + url + '" controls autoplay loop></video>';
            } else {
                modalMedia.innerHTML = '<img class="modal-media" src="' + url + '" alt="Full size image">';
            }
            
            modalInfo.innerHTML = '<span class="username">' + user + '</span> • ' + date;
        }
        
        function closeModal() {
            const modal = document.getElementById('imageModal');
            modal.classList.remove('active');
            document.body.style.overflow = '';
            
            // Stop video playback if any
            const video = modal.querySelector('video');
            if (video) {
                video.pause();
            }
        }
        
        function navigateModal(direction, event) {
            if (event) {
                event.stopPropagation();
            }
            currentIndex += direction;
            
            if (currentIndex < 0) {
                currentIndex = currentCards.length - 1;
            } else if (currentIndex >= currentCards.length) {
                currentIndex = 0;
            }
            
            showModalContent(currentCards[currentIndex]);
        }
        
        function handleModalClick(event) {
            if (event.target.id === 'imageModal') {
                closeModal();
            }
        }
        
        // Keyboard navigation
        document.addEventListener('keydown', function(e) {
            const modal = document.getElementById('imageModal');
            if (!modal.classList.contains('active')) return;
            
            switch(e.key) {
                case 'Escape':
                    closeModal();
                    break;
                case 'ArrowLeft':
                    navigateModal(-1);
                    break;
                case 'ArrowRight':
                    navigateModal(1);
                    break;
            }
        });
    </script>
</body>
</html>`

	// Prepare template data
	type TemplateData struct {
		Galleries []struct {
			Username string
			Images   []galleryImageDisplay
		}
		PrunedImages []PrunedImageDisplay
		TotalUsers   int
		TotalImages  int
		UpdateTime   string
	}

	data := TemplateData{
		TotalUsers:  len(galleries),
		TotalImages: totalImages,
		UpdateTime:  time.Now().Format("2006-01-02 15:04:05"),
	}

	// Add pruned images to template data
	for _, img := range prunedImages {
		deadSince := img.PostedAt.Format("2006-01-02 15:04")
		if img.LastCheckAt != nil {
			deadSince = img.LastCheckAt.Format("2006-01-02 15:04")
		}

		// Mark as gravestone if dead for over 48 hours
		isGravestone := false
		if img.LastCheckAt != nil && time.Since(*img.LastCheckAt) > 48*time.Hour {
			isGravestone = true
		}

		data.PrunedImages = append(data.PrunedImages, PrunedImageDisplay{
			Username:     img.Username,
			URL:          img.URL,
			PrunedReason: getString(img.PrunedReason),
			DeadSince:    deadSince,
			IsGravestone: isGravestone,
		})
	}

	for _, gallery := range galleries {
		var displayImages []galleryImageDisplay
		for _, img := range gallery.Images {
			mediaType := getMediaType(img.URL)
			displayImages = append(displayImages, galleryImageDisplay{
				URL:               img.URL,
				Username:          img.Username,
				PostedAtFormatted: img.PostedAt.Format("2006-01-02 15:04"),
				ImageTitle:        getString(img.ImageTitle),
				OriginalPoster:    getString(img.OriginalPoster),
				IsVideo:           mediaType == "video",
				MediaType:         mediaType,
			})
		}
		data.Galleries = append(data.Galleries, struct {
			Username string
			Images   []galleryImageDisplay
		}{
			Username: gallery.Username,
			Images:   displayImages,
		})
	}

	// Parse and execute template
	tmpl, err := template.New("shared-gallery").Parse(tmplStr)
	if err != nil {
		logger.Error("gallery", "Failed to parse shared gallery template: %v", err)
		return "<html><body>Error generating gallery</body></html>"
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		logger.Error("gallery", "Failed to execute shared gallery template: %v", err)
		return "<html><body>Error generating gallery</body></html>"
	}

	return result.String()
}

const galleryMarkerFile = ".daz-gallery"

func (g *HTMLGenerator) markerFilePath() string {
	return filepath.Join(filepath.Clean(g.config.HTMLOutputPath), galleryMarkerFile)
}

func (g *HTMLGenerator) ensureMarkerFile() error {
	if !g.isSafeOutputPath() {
		return fmt.Errorf("unsafe html output path: %s", g.config.HTMLOutputPath)
	}
	markerPath := g.markerFilePath()
	gitDir := filepath.Join(filepath.Clean(g.config.HTMLOutputPath), ".git")
	if _, err := os.Stat(gitDir); err == nil {
		if _, err := os.Stat(markerPath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("gallery marker missing in existing git directory: %s", g.config.HTMLOutputPath)
			}
			return fmt.Errorf("failed to stat gallery marker: %w", err)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat gallery git directory: %w", err)
	}
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat gallery marker: %w", err)
	}
	return os.WriteFile(markerPath, []byte("daz gallery output"), 0644)
}

func (g *HTMLGenerator) hasMarkerFile() bool {
	if _, err := os.Stat(g.markerFilePath()); err != nil {
		return false
	}
	return true
}

func (g *HTMLGenerator) isSafeOutputPath() bool {
	outputPath := filepath.Clean(g.config.HTMLOutputPath)
	if outputPath == "" || outputPath == "." || outputPath == string(filepath.Separator) {
		return false
	}
	if !filepath.IsAbs(outputPath) {
		return false
	}
	return true
}

func (g *HTMLGenerator) isSafeGitDir() bool {
	if !g.isSafeOutputPath() {
		return false
	}
	if !g.hasMarkerFile() {
		return false
	}
	gitDir := filepath.Join(filepath.Clean(g.config.HTMLOutputPath), ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return false
	}
	return true
}

// resetGitState resets git repository to clean state after failure
func (g *HTMLGenerator) resetGitState() {
	if !g.isSafeGitDir() {
		logger.Warn("gallery", "Skipping git reset/clean for unsafe output path: %s", g.config.HTMLOutputPath)
		return
	}

	// Try to reset to clean state
	cmd := exec.Command("git", "reset", "--hard", "HEAD")
	cmd.Dir = g.config.HTMLOutputPath
	if err := cmd.Run(); err != nil {
		logger.Error("gallery", "Failed to reset git state: %v", err)
	}

	// Clean untracked files
	cmd = exec.Command("git", "clean", "-fd")
	cmd.Dir = g.config.HTMLOutputPath
	if err := cmd.Run(); err != nil {
		logger.Error("gallery", "Failed to clean git directory: %v", err)
	}
}

// pushToGitHub commits and pushes gallery HTML to GitHub Pages
func (g *HTMLGenerator) pushToGitHub() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Recovery function to reset git state on failure
	defer func() {
		if r := recover(); r != nil {
			logger.Error("gallery", "Panic during git push: %v", r)
			// Try to reset git state
			g.resetGitState()
		}
	}()

	if !g.isSafeOutputPath() {
		return fmt.Errorf("unsafe html output path: %s", g.config.HTMLOutputPath)
	}
	if err := g.ensureMarkerFile(); err != nil {
		return fmt.Errorf("failed to write gallery marker file: %w", err)
	}

	// Track if we need recovery on normal errors

	var needsRecovery bool
	defer func() {
		if needsRecovery {
			logger.Warn("gallery", "Git operation failed, resetting state")
			g.resetGitState()
		}
	}()

	// Get GitHub token once
	githubToken := os.Getenv("GITHUB_TOKEN")

	// Helper function to run git commands
	runGitCmd := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = g.config.HTMLOutputPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Some commands like "git remote get-url" may fail normally
			return fmt.Errorf("git %v failed: %w, output: %s", args, err, string(output))
		}
		return nil
	}

	// Helper for authenticated push only
	runAuthPush := func() error {
		if githubToken == "" {
			return fmt.Errorf("no GitHub token configured")
		}

		// Use token directly in URL for authentication
		authURL := fmt.Sprintf("https://x-access-token:%s@github.com/hildolfr/daz.git", githubToken)
		cmd := exec.CommandContext(ctx, "git", "push", authURL, "gh-pages", "--force")
		cmd.Dir = g.config.HTMLOutputPath

		// Suppress credential prompts
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0")

		output, err := cmd.CombinedOutput()
		if err != nil {
			// Don't include token in error message
			return fmt.Errorf("push failed: %w", err)
		}

		logger.Debug("gallery", "Push output: %s", string(output))
		return nil
	}

	// Check if git repo already exists
	gitDir := filepath.Join(g.config.HTMLOutputPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Initialize git repo
		if err := runGitCmd("init"); err != nil {
			return fmt.Errorf("failed to init git repo: %w", err)
		}

		// Set git config for this repo (only needed on first init)
		if err := runGitCmd("config", "user.name", "hildolfr"); err != nil {
			return fmt.Errorf("failed to set git user: %w", err)
		}
		if err := runGitCmd("config", "user.email", "svhildolfr@gmail.com"); err != nil {
			return fmt.Errorf("failed to set git email: %w", err)
		}
	}

	// Use clean URL without token
	remoteURL := "https://github.com/hildolfr/daz.git"

	// Check if remote exists
	if err := runGitCmd("remote", "get-url", "origin"); err != nil {
		// Remote doesn't exist, add it
		if err := runGitCmd("remote", "add", "origin", remoteURL); err != nil {
			return fmt.Errorf("failed to add remote: %w", err)
		}
	} else {
		// Update remote URL to ensure no token in URL
		if err := runGitCmd("remote", "set-url", "origin", remoteURL); err != nil {
			return fmt.Errorf("failed to update remote: %w", err)
		}
	}

	// Switch to gh-pages branch
	if err := runGitCmd("checkout", "-B", "gh-pages"); err != nil {
		needsRecovery = true
		return fmt.Errorf("failed to checkout gh-pages: %w", err)
	}

	// Add all files
	if err := runGitCmd("add", "-A"); err != nil {
		needsRecovery = true
		return fmt.Errorf("failed to add files: %w", err)
	}

	// Commit with timestamp
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	commitMsg := fmt.Sprintf("Update galleries: %s", timestamp)
	if err := runGitCmd("commit", "-m", commitMsg); err != nil {
		// Ignore error if nothing to commit
		if !strings.Contains(err.Error(), "nothing to commit") {
			logger.Debug("gallery", "Commit result: %v", err)
		}
	}

	// Push to GitHub Pages using authenticated push
	if err := runAuthPush(); err != nil {
		needsRecovery = true
		return fmt.Errorf("failed to push to GitHub: %w", err)
	}

	logger.Debug("gallery", "Successfully pushed to GitHub Pages")
	return nil
}
