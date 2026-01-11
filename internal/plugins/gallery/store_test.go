package gallery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type galleryEventBus struct {
	requests     []galleryRequest
	handler      func(galleryRequest) (*framework.EventData, error)
	execRequests int
}

type galleryRequest struct {
	target    string
	eventType string
	data      *framework.EventData
	metadata  *framework.EventMetadata
}

func (g *galleryEventBus) Broadcast(eventType string, data *framework.EventData) error {
	return nil
}

func (g *galleryEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (g *galleryEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (g *galleryEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (g *galleryEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	request := galleryRequest{
		target:    target,
		eventType: eventType,
		data:      data,
		metadata:  metadata,
	}
	g.requests = append(g.requests, request)
	if eventType == "sql.exec.request" {
		g.execRequests++
	}

	if g.handler != nil {
		return g.handler(request)
	}

	if eventType == "sql.query.request" && data.SQLQueryRequest != nil {
		return &framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				ID:            data.SQLQueryRequest.ID,
				CorrelationID: data.SQLQueryRequest.CorrelationID,
				Success:       true,
				Columns:       []string{"result"},
				Rows:          [][]json.RawMessage{},
			},
		}, nil
	}
	if eventType == "sql.exec.request" && data.SQLExecRequest != nil {
		return &framework.EventData{
			SQLExecResponse: &framework.SQLExecResponse{
				ID:            data.SQLExecRequest.ID,
				CorrelationID: data.SQLExecRequest.CorrelationID,
				Success:       true,
				RowsAffected:  1,
			},
		}, nil
	}

	return nil, nil
}

func (g *galleryEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (g *galleryEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (g *galleryEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (g *galleryEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (g *galleryEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (g *galleryEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (g *galleryEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestAddImageIncludesMaxImages(t *testing.T) {
	bus := &galleryEventBus{}
	maxImages := 10
	store := NewStore(bus, "gallery_test", maxImages)

	bus.handler = func(request galleryRequest) (*framework.EventData, error) {
		if request.eventType != "sql.query.request" {
			t.Fatalf("expected sql.query.request, got %s", request.eventType)
		}

		query := request.data.SQLQueryRequest
		if query == nil {
			t.Fatal("expected SQLQueryRequest")
		}

		if strings.Contains(query.Query, "FROM daz_gallery_locks") {
			lockedBytes, err := json.Marshal(false)
			if err != nil {
				return nil, err
			}

			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            query.ID,
					CorrelationID: query.CorrelationID,
					Success:       true,
					Columns:       []string{"exists"},
					Rows:          [][]json.RawMessage{{lockedBytes}},
				},
			}, nil
		}

		if query.Query == "SELECT add_gallery_image($1, $2, $3, NULL, $4)" {
			if len(query.Params) != 4 {
				t.Fatalf("expected 4 params, got %d", len(query.Params))
			}
			if query.Params[3].Value != maxImages {
				t.Fatalf("expected maxImages %d, got %v", maxImages, query.Params[3].Value)
			}

			idBytes, err := json.Marshal(int64(42))
			if err != nil {
				return nil, err
			}

			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            query.ID,
					CorrelationID: query.CorrelationID,
					Success:       true,
					Columns:       []string{"add_gallery_image"},
					Rows:          [][]json.RawMessage{{idBytes}},
				},
			}, nil
		}

		t.Fatalf("unexpected query: %s", query.Query)

		return nil, nil
	}

	if err := store.AddImage("tester", "https://example.com/image.jpg", "room"); err != nil {
		t.Fatalf("AddImage failed: %v", err)
	}
}

func TestRestoreDeadImageIncludesMaxImages(t *testing.T) {
	bus := &galleryEventBus{}
	maxImages := 12
	store := NewStore(bus, "gallery_test", maxImages)
	imageID := int64(99)

	bus.handler = func(request galleryRequest) (*framework.EventData, error) {
		if request.eventType != "sql.query.request" {
			t.Fatalf("expected sql.query.request, got %s", request.eventType)
		}

		query := request.data.SQLQueryRequest
		if query == nil {
			t.Fatal("expected SQLQueryRequest")
		}
		if query.Query != "SELECT restore_dead_image($1, $2)" {
			t.Fatalf("unexpected query: %s", query.Query)
		}
		if len(query.Params) != 2 {
			t.Fatalf("expected 2 params, got %d", len(query.Params))
		}
		if query.Params[0].Value != imageID {
			t.Fatalf("expected image ID %d, got %v", imageID, query.Params[0].Value)
		}
		if query.Params[1].Value != maxImages {
			t.Fatalf("expected maxImages %d, got %v", maxImages, query.Params[1].Value)
		}

		restoredBytes, err := json.Marshal(true)
		if err != nil {
			return nil, err
		}

		return &framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				ID:            query.ID,
				CorrelationID: query.CorrelationID,
				Success:       true,
				Columns:       []string{"restore_dead_image"},
				Rows:          [][]json.RawMessage{{restoredBytes}},
			},
		}, nil
	}

	restored, err := store.RestoreDeadImage(imageID)
	if err != nil {
		t.Fatalf("RestoreDeadImage failed: %v", err)
	}
	if !restored {
		t.Fatal("expected restored to be true")
	}
}

func TestRecoverDeadImageUsesMaxImages(t *testing.T) {
	bus := &galleryEventBus{}
	maxImages := 8
	store := NewStore(bus, "gallery_test", maxImages)
	imageID := int64(7)
	step := 0

	bus.handler = func(request galleryRequest) (*framework.EventData, error) {
		switch step {
		case 0:
			step++
			query := request.data.SQLQueryRequest
			if request.eventType != "sql.query.request" || query == nil {
				t.Fatalf("expected initial sql.query.request")
			}
			if query.Query != "SELECT is_active FROM daz_gallery_images WHERE id = $1" {
				t.Fatalf("unexpected query: %s", query.Query)
			}
			if len(query.Params) != 1 || query.Params[0].Value != imageID {
				t.Fatalf("unexpected params for active check")
			}
			inactiveBytes, err := json.Marshal(false)
			if err != nil {
				return nil, err
			}
			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            query.ID,
					CorrelationID: query.CorrelationID,
					Success:       true,
					Columns:       []string{"is_active"},
					Rows:          [][]json.RawMessage{{inactiveBytes}},
				},
			}, nil
		case 1:
			step++
			query := request.data.SQLQueryRequest
			if request.eventType != "sql.query.request" || query == nil {
				t.Fatalf("expected recovery sql.query.request")
			}
			if query.Query != "SELECT restore_dead_image($1, $2)" {
				t.Fatalf("unexpected recovery query: %s", query.Query)
			}
			if len(query.Params) != 2 || query.Params[1].Value != maxImages {
				t.Fatalf("expected maxImages %d, got %v", maxImages, query.Params[1].Value)
			}
			recoveredBytes, err := json.Marshal(true)
			if err != nil {
				return nil, err
			}
			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            query.ID,
					CorrelationID: query.CorrelationID,
					Success:       true,
					Columns:       []string{"restore_dead_image"},
					Rows:          [][]json.RawMessage{{recoveredBytes}},
				},
			}, nil
		default:
			return nil, nil
		}
	}

	recovered, err := store.RecoverDeadImage(imageID)
	if err != nil {
		t.Fatalf("RecoverDeadImage failed: %v", err)
	}
	if !recovered {
		t.Fatal("expected recovered to be true")
	}
	if bus.execRequests != 0 {
		t.Fatalf("expected no exec requests, got %d", bus.execRequests)
	}
}

func TestPrunedImageDisplay(t *testing.T) {
	display := PrunedImageDisplay{
		Username:     "testuser",
		URL:          "https://example.com/image.jpg",
		PrunedReason: "404 Not Found",
		DeadSince:    "2025-08-07 12:00",
		IsGravestone: false,
	}

	if display.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got %s", display.Username)
	}

	if display.URL != "https://example.com/image.jpg" {
		t.Errorf("Expected URL 'https://example.com/image.jpg', got %s", display.URL)
	}

	if display.IsGravestone {
		t.Error("Expected IsGravestone to be false for recent death")
	}

	oldDisplay := PrunedImageDisplay{
		Username:     "olduser",
		URL:          "https://example.com/old.jpg",
		PrunedReason: "Gone",
		DeadSince:    time.Now().Add(-72 * time.Hour).Format("2006-01-02 15:04"),
		IsGravestone: true,
	}

	if !oldDisplay.IsGravestone {
		t.Error("Expected IsGravestone to be true for old death")
	}
}
