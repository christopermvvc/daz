package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	config    Config
	pool      *pgxpool.Pool
	logPool   *pgxpool.Pool
	db        *sql.DB
	ctx       context.Context
	cancel    context.CancelFunc
	startTime time.Time
	readyChan chan struct{}
	workerWg  sync.WaitGroup

	loggerRules   []LoggerRule
	eventsHandled int64
	idCounter     int64

	coreEventLogQueue   chan *coreEventLogEntry
	coreEventBatchSize  int
	coreEventFlushEvery time.Duration
	coreEventDrops      atomic.Int64
	coreEventCopyErrors atomic.Int64
	coreEventCopyFn     func(context.Context, []*coreEventLogEntry) error
	coreEventExecFn     func(context.Context, string, ...interface{}) error

	sqlCriticalSem     chan struct{}
	sqlBackgroundSem   chan struct{}
	sqlBackgroundPend  chan struct{}
	sqlBackgroundDrops atomic.Int64

	pgStatAvailable atomic.Bool
}

type Config struct {
	Database                  DatabaseConfig `json:"database"`
	LoggerRules               []LoggerRule   `json:"logger_rules"`
	BackgroundMaxConnections  int            `json:"background_max_connections"`
	CriticalLaneConcurrency   int            `json:"critical_lane_concurrency"`
	BackgroundLaneConcurrency int            `json:"background_lane_concurrency"`
	BackgroundQueueMax        int            `json:"background_queue_max"`
	BackgroundQueueWaitMS     int            `json:"background_queue_wait_ms"`
	CoreEventQueueSize        int            `json:"core_event_queue_size"`
	CoreEventBatchSize        int            `json:"core_event_batch_size"`
	CoreEventFlushMS          int            `json:"core_event_flush_ms"`
	EnablePGStatStatements    *bool          `json:"enable_pg_stat_statements,omitempty"`
	PGStatReportIntervalSec   int            `json:"pg_stat_report_interval_seconds"`
	PGStatTopN                int            `json:"pg_stat_top_n"`
}

type DatabaseConfig struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Database        string `json:"database"`
	User            string `json:"user"`
	Password        string `json:"password"`
	MaxConnections  int    `json:"max_connections"`
	MaxConnLifetime int    `json:"max_conn_lifetime"`
	ConnectTimeout  int    `json:"connect_timeout"`
}

type LoggerRule struct {
	EventPattern string   `json:"event_pattern"`
	Enabled      bool     `json:"enabled"`
	Table        string   `json:"table"`
	Fields       []string `json:"fields,omitempty"`
	Transform    string   `json:"transform,omitempty"`
	regex        *regexp.Regexp
}

// LogFields represents all possible fields that can be logged from EventData
type LogFields struct {
	EventType   string    `json:"event_type"`
	Timestamp   time.Time `json:"timestamp"`
	Username    string    `json:"username,omitempty"`
	Message     string    `json:"message,omitempty"`
	UserRank    int       `json:"user_rank,omitempty"`
	UserID      string    `json:"user_id,omitempty"`
	Channel     string    `json:"channel,omitempty"`
	VideoID     string    `json:"video_id,omitempty"`
	VideoType   string    `json:"video_type,omitempty"`
	Title       string    `json:"title,omitempty"`
	Duration    int       `json:"duration,omitempty"`
	ToUser      string    `json:"to_user,omitempty"`
	MessageTime int64     `json:"message_time,omitempty"`
}

type coreEventLogEntry struct {
	EventType   string
	Timestamp   time.Time
	Channel     string
	Username    string
	Message     string
	MessageTime int64
	ToUser      string
	VideoID     string
	VideoType   string
	Title       string
	Duration    int
	RawData     []byte
}

const (
	defaultCoreEventQueueSize       = 4000
	defaultCoreEventBatchSize       = 64
	defaultCoreEventFlushMS         = 200
	coreEventCriticalEnqueueMS      = 75
	defaultSQLBackgroundQueueWaitMS = 1200
	defaultSQLBackgroundQueueFactor = 8
	lowCPUBackgroundQueueFactor     = 4
)

var backgroundSQLSources = map[string]struct{}{
	"analytics":    {},
	"gallery":      {},
	"mediatracker": {},
	"retry":        {},
	"usertracker":  {},
}

func NewPlugin() *Plugin {
	return &Plugin{
		name:      "sql",
		readyChan: make(chan struct{}),
	}
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	return []string{} // SQL plugin has no dependencies
}

// Ready returns true when the plugin is ready to accept requests
func (p *Plugin) Ready() bool {
	select {
	case <-p.readyChan:
		return true
	default:
		return false
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Init(configData json.RawMessage, bus framework.EventBus) error {
	if err := json.Unmarshal(configData, &p.config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	p.eventBus = bus

	for i := range p.config.LoggerRules {
		if p.config.LoggerRules[i].EventPattern != "" {
			pattern := p.config.LoggerRules[i].EventPattern
			pattern = strings.ReplaceAll(pattern, ".", "\\.")
			pattern = strings.ReplaceAll(pattern, "*", ".*")
			pattern = "^" + pattern + "$"

			regex, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("invalid event pattern %s: %w", p.config.LoggerRules[i].EventPattern, err)
			}
			p.config.LoggerRules[i].regex = regex
		}
	}

	p.loggerRules = p.config.LoggerRules
	if p.config.CoreEventQueueSize <= 0 {
		p.config.CoreEventQueueSize = defaultCoreEventQueueSize
	}
	if p.config.CoreEventBatchSize <= 0 {
		p.config.CoreEventBatchSize = defaultCoreEventBatchSize
	}
	if p.config.CoreEventFlushMS <= 0 {
		p.config.CoreEventFlushMS = defaultCoreEventFlushMS
	}

	p.coreEventBatchSize = p.config.CoreEventBatchSize
	p.coreEventFlushEvery = time.Duration(p.config.CoreEventFlushMS) * time.Millisecond
	p.coreEventLogQueue = make(chan *coreEventLogEntry, p.config.CoreEventQueueSize)
	p.coreEventCopyFn = p.copyCoreEventBatch
	p.coreEventExecFn = p.execLog

	if p.config.PGStatTopN <= 0 {
		p.config.PGStatTopN = 5
	}

	logger.Debug("SQL", "Initialized with %d logger rules", len(p.loggerRules))
	return nil
}

func (p *Plugin) Start() error {
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.startTime = time.Now()

	// Don't subscribe to events until database is connected
	// This prevents handling requests before we're ready

	// Connect to database in a goroutine to avoid blocking
	go func() {
		logger.Debug("SQL", "Starting database connection...")
		startTime := time.Now()

		if err := p.connectDatabase(); err != nil {
			logger.Error("SQL", "Failed to connect to database after %v: %v", time.Since(startTime), err)
			// Don't close readyChan on error, let handlers return errors
			return
		}

		logger.Debug("SQL", "Database connected successfully after %v", time.Since(startTime))

		// Subscribe to event handlers after database is connected
		logger.Debug("SQL", "Subscribing to event handlers...")

		if err := p.eventBus.Subscribe("log.request", p.handleLogRequest); err != nil {
			logger.Error("SQL", "Failed to subscribe to log.request: %v", err)
			return
		}
		logger.Debug("SQL", "Subscribed to log.request")

		if err := p.eventBus.Subscribe("log.batch", p.handleBatchLogRequest); err != nil {
			logger.Error("SQL", "Failed to subscribe to log.batch: %v", err)
			return
		}
		logger.Debug("SQL", "Subscribed to log.batch")

		if err := p.eventBus.Subscribe("log.configure", p.handleConfigureLogging); err != nil {
			logger.Error("SQL", "Failed to subscribe to log.configure: %v", err)
			return
		}
		logger.Debug("SQL", "Subscribed to log.configure")

		// Subscribe to plugin.request for ALL synchronous requests
		// This is the standard pattern for handling eventBus.Request() calls
		if err := p.eventBus.Subscribe("plugin.request", p.handlePluginRequest); err != nil {
			logger.Error("SQL", "Failed to subscribe to plugin.request: %v", err)
			return
		}
		logger.Debug("SQL", "Subscribed to plugin.request")

		// NOTE: All SQL synchronous requests (query, exec, batch) are handled through plugin.request
		// The handlePluginRequest function inspects the EventData and routes to the appropriate handler:
		// - SQLQueryRequest -> handleSQLQuery
		// - SQLExecRequest -> handleSQLExec
		// - SQLBatchRequest -> handleBatchQueryRequest or handleBatchExecRequest

		// Dedicated SQL request lanes keep command-critical SQL away from generic plugin.request pressure.
		if err := p.eventBus.Subscribe("sql.query.request", p.handleSQLQuery); err != nil {
			logger.Error("SQL", "Failed to subscribe to sql.query.request: %v", err)
			return
		}
		if err := p.eventBus.Subscribe("sql.exec.request", p.handleSQLExec); err != nil {
			logger.Error("SQL", "Failed to subscribe to sql.exec.request: %v", err)
			return
		}
		if err := p.eventBus.Subscribe("sql.batch.request", p.handleBatchExecRequest); err != nil {
			logger.Error("SQL", "Failed to subscribe to sql.batch.request: %v", err)
			return
		}

		// Subscribe to logger rules after database is connected
		for _, rule := range p.loggerRules {
			if rule.Enabled && rule.regex != nil {
				logger.Debug("SQL", "Subscribing to pattern: %s", rule.EventPattern)
				for _, eventType := range p.findMatchingEventTypes(rule.EventPattern) {
					if err := p.eventBus.Subscribe(eventType, p.createLoggerHandler(rule)); err != nil {
						logger.Error("SQL", "Failed to subscribe to %s: %v", eventType, err)
					}
				}
			}
		}

		p.startCoreEventWriter()
		p.startPGStatReporter()

		// Signal that the plugin is ready after database is connected
		close(p.readyChan)
		logger.Debug("SQL", "Started successfully and ready to accept requests after %v total startup time", time.Since(startTime))
	}()

	logger.Debug("SQL", "Started (connecting to database in background)")
	return nil
}

func (p *Plugin) Stop() error {
	p.cancel()
	p.workerWg.Wait()

	if p.db != nil {
		if err := p.db.Close(); err != nil {
			logger.Error("SQL", "Failed to close database connection: %v", err)
		}
	}
	if p.logPool != nil && p.logPool != p.pool {
		p.logPool.Close()
	}
	if p.pool != nil {
		p.pool.Close()
	}

	logger.Info("SQL", "Stopped")
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	p.eventsHandled++
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	return framework.PluginStatus{
		Name:          p.name,
		State:         "running",
		LastError:     nil,
		RetryCount:    0,
		EventsHandled: p.eventsHandled,
		Uptime:        time.Since(p.startTime),
	}
}

// emitFailureEvent emits a failure event that can be picked up by the retry plugin
func (p *Plugin) emitFailureEvent(eventType, correlationID, source, operationType string, err error) {
	failureData := &framework.EventData{
		KeyValue: map[string]string{
			"correlation_id": correlationID,
			"source":         source,
			"operation_type": operationType,
			"error":          err.Error(),
			"timestamp":      time.Now().Format(time.RFC3339),
		},
	}

	// Emit the failure event asynchronously
	go func() {
		if err := p.eventBus.Broadcast(eventType, failureData); err != nil {
			logger.Error("SQL", "Failed to emit failure event: %v", err)
		}
	}()
}

func (p *Plugin) connectDatabase() error {
	ctx, cancel := context.WithTimeout(p.ctx, time.Duration(p.config.Database.ConnectTimeout)*time.Second)
	defer cancel()

	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		p.config.Database.Host,
		p.config.Database.Port,
		p.config.Database.User,
		p.config.Database.Password,
		p.config.Database.Database,
	)

	logger.Debug("SQL", "Connecting to database at %s:%d/%s",
		p.config.Database.Host, p.config.Database.Port, p.config.Database.Database)

	totalConns := p.config.Database.MaxConnections
	if totalConns <= 0 {
		totalConns = 10
	}
	criticalConns, backgroundConns := splitConnectionBudget(totalConns, p.config.BackgroundMaxConnections)
	p.configureSQLRequestLanes(criticalConns, backgroundConns)
	metrics.SQLPoolConfiguredConnections.WithLabelValues("critical").Set(float64(criticalConns))
	metrics.SQLPoolConfiguredConnections.WithLabelValues("background").Set(float64(backgroundConns))

	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	poolConfig.MaxConns = int32(criticalConns)
	poolConfig.MaxConnLifetime = time.Duration(p.config.Database.MaxConnLifetime) * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	p.pool = pool
	p.logPool = pool

	if backgroundConns > 0 {
		logPoolConfig, err := pgxpool.ParseConfig(connStr)
		if err != nil {
			return fmt.Errorf("failed to parse background pool config: %w", err)
		}
		logPoolConfig.MaxConns = int32(backgroundConns)
		logPoolConfig.MaxConnLifetime = time.Duration(p.config.Database.MaxConnLifetime) * time.Second

		logPool, err := pgxpool.NewWithConfig(ctx, logPoolConfig)
		if err != nil {
			p.pool.Close()
			return fmt.Errorf("failed to create background pool: %w", err)
		}
		if err := logPool.Ping(ctx); err != nil {
			logPool.Close()
			p.pool.Close()
			return fmt.Errorf("failed to ping background pool: %w", err)
		}
		p.logPool = logPool
	}

	logger.Debug("SQL", "Initializing database schema...")
	schemaStart := time.Now()
	if err := p.initializeSchema(ctx); err != nil {
		if p.logPool != nil && p.logPool != p.pool {
			p.logPool.Close()
		}
		p.pool.Close()
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	logger.Debug("SQL", "Schema initialization completed in %v", time.Since(schemaStart))

	p.logPostgresTuningDiagnostics(ctx)
	p.initializePGStatStatements(ctx)

	logger.Debug("SQL", "Connected to database successfully (critical_conns=%d background_conns=%d)", criticalConns, backgroundConns)
	return nil
}

func splitConnectionBudget(total int, configuredBackground int) (critical int, background int) {
	return splitConnectionBudgetForCPU(total, configuredBackground, runtime.GOMAXPROCS(0))
}

func splitConnectionBudgetForCPU(total int, configuredBackground int, gomaxprocs int) (critical int, background int) {
	if total <= 1 {
		return 1, 0
	}

	background = configuredBackground
	if background <= 0 {
		background = defaultBackgroundConnections(total, gomaxprocs)
	}

	if background >= total {
		background = total - 1
	}
	if background < 0 {
		background = 0
	}

	critical = total - background
	if critical < 1 {
		critical = 1
		background = total - 1
	}

	return critical, background
}

func defaultBackgroundConnections(total int, gomaxprocs int) int {
	if total <= 2 {
		return 0
	}

	if gomaxprocs <= 1 {
		// Keep low-CPU hosts heavily biased toward command/critical capacity.
		background := total / 8
		if background < 1 {
			background = 1
		}
		if background > 2 {
			background = 2
		}
		if background >= total {
			background = total - 1
		}
		return background
	}

	background := total / 5
	if background < 1 {
		background = 1
	}
	if background > 4 {
		background = 4
	}
	if background >= total {
		background = total - 1
	}
	return background
}

func defaultCriticalLaneConcurrency(criticalConns int, gomaxprocs int) int {
	if criticalConns <= 0 {
		return 1
	}

	switch {
	case gomaxprocs <= 1:
		if criticalConns > 2 {
			return 2
		}
	case gomaxprocs == 2:
		if criticalConns > 4 {
			return 4
		}
	default:
		limit := gomaxprocs * 2
		if limit < 4 {
			limit = 4
		}
		if criticalConns > limit {
			return limit
		}
	}

	return criticalConns
}

func defaultBackgroundLaneConcurrency(backgroundConns int, gomaxprocs int) int {
	if backgroundConns <= 0 {
		return 0
	}

	switch {
	case gomaxprocs <= 1:
		if backgroundConns > 1 {
			return 1
		}
	case gomaxprocs == 2:
		if backgroundConns > 2 {
			return 2
		}
	default:
		if backgroundConns > gomaxprocs {
			return gomaxprocs
		}
	}

	return backgroundConns
}

func defaultBackgroundQueueMax(backgroundConns int, gomaxprocs int) int {
	if backgroundConns <= 0 {
		return 0
	}

	factor := defaultSQLBackgroundQueueFactor
	if gomaxprocs <= 1 {
		factor = lowCPUBackgroundQueueFactor
	}

	queueMax := backgroundConns * factor
	if queueMax < backgroundConns {
		queueMax = backgroundConns
	}
	return queueMax
}

func (p *Plugin) configureSQLRequestLanes(criticalConns, backgroundConns int) {
	if criticalConns < 1 {
		criticalConns = 1
	}
	if backgroundConns < 0 {
		backgroundConns = 0
	}

	criticalLaneConcurrency := p.config.CriticalLaneConcurrency
	if criticalLaneConcurrency <= 0 || criticalLaneConcurrency > criticalConns {
		criticalLaneConcurrency = defaultCriticalLaneConcurrency(criticalConns, runtime.GOMAXPROCS(0))
	}
	p.sqlCriticalSem = make(chan struct{}, criticalLaneConcurrency)

	if backgroundConns == 0 {
		p.sqlBackgroundSem = nil
		p.sqlBackgroundPend = nil
		p.observeSQLLaneMetrics()
		return
	}

	backgroundLaneConcurrency := p.config.BackgroundLaneConcurrency
	if backgroundLaneConcurrency <= 0 || backgroundLaneConcurrency > backgroundConns {
		backgroundLaneConcurrency = defaultBackgroundLaneConcurrency(backgroundConns, runtime.GOMAXPROCS(0))
	}
	p.sqlBackgroundSem = make(chan struct{}, backgroundLaneConcurrency)

	queueMax := p.config.BackgroundQueueMax
	if queueMax <= 0 {
		queueMax = defaultBackgroundQueueMax(backgroundConns, runtime.GOMAXPROCS(0))
	}
	p.sqlBackgroundPend = make(chan struct{}, queueMax)
	p.observeSQLLaneMetrics()
}

type sqlRequestLane int

const (
	sqlLaneCritical sqlRequestLane = iota
	sqlLaneBackground
)

func (p *Plugin) classifySQLRequestLane(source, query string) sqlRequestLane {
	normalizedSource := strings.ToLower(strings.TrimSpace(source))
	if _, background := backgroundSQLSources[normalizedSource]; background {
		return sqlLaneBackground
	}

	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(lowerQuery, "daz_core_events") ||
		strings.Contains(lowerQuery, "daz_chat_log") ||
		strings.Contains(lowerQuery, "daz_user_activity") {
		return sqlLaneBackground
	}

	return sqlLaneCritical
}

func (p *Plugin) acquireSQLRequestLane(ctx context.Context, source, query string) (func(), error) {
	if p.sqlCriticalSem == nil {
		return func() {}, nil
	}

	lane := p.classifySQLRequestLane(source, query)
	if lane == sqlLaneCritical || p.sqlBackgroundSem == nil {
		select {
		case p.sqlCriticalSem <- struct{}{}:
			p.observeSQLLaneMetrics()
			return func() {
				<-p.sqlCriticalSem
				p.observeSQLLaneMetrics()
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	select {
	case p.sqlBackgroundSem <- struct{}{}:
		p.observeSQLLaneMetrics()
		return func() {
			<-p.sqlBackgroundSem
			p.observeSQLLaneMetrics()
		}, nil
	default:
	}

	if p.sqlBackgroundPend == nil {
		p.recordBackgroundSQLDrop(source, query)
		return nil, fmt.Errorf("background SQL lane saturated")
	}

	select {
	case p.sqlBackgroundPend <- struct{}{}:
		p.observeSQLLaneMetrics()
	default:
		p.recordBackgroundSQLDrop(source, query)
		return nil, fmt.Errorf("background SQL lane saturated")
	}
	defer func() {
		<-p.sqlBackgroundPend
		p.observeSQLLaneMetrics()
	}()

	waitMS := p.config.BackgroundQueueWaitMS
	if waitMS <= 0 {
		waitMS = defaultSQLBackgroundQueueWaitMS
	}

	waitCtx := ctx
	cancel := func() {}
	if waitMS > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, time.Duration(waitMS)*time.Millisecond)
	}
	defer cancel()

	select {
	case p.sqlBackgroundSem <- struct{}{}:
		p.observeSQLLaneMetrics()
		return func() {
			<-p.sqlBackgroundSem
			p.observeSQLLaneMetrics()
		}, nil
	case <-waitCtx.Done():
		p.recordBackgroundSQLDrop(source, query)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("background SQL lane wait timed out")
	}
}

func (p *Plugin) recordBackgroundSQLDrop(source, query string) {
	drops := p.sqlBackgroundDrops.Add(1)
	metrics.SQLLaneDrops.WithLabelValues("background").Inc()
	if drops == 1 || drops%100 == 0 {
		trimmedQuery := strings.TrimSpace(query)
		if len(trimmedQuery) > 80 {
			trimmedQuery = trimmedQuery[:80] + "..."
		}
		logger.Warn("SQL", "Dropping/deprioritizing background SQL request under pressure (source=%s dropped=%d query=%q)", source, drops, trimmedQuery)
	}
}

func (p *Plugin) getLogPool() *pgxpool.Pool {
	if p.logPool != nil {
		return p.logPool
	}
	return p.pool
}

func (p *Plugin) execLog(ctx context.Context, query string, args ...interface{}) error {
	pool := p.getLogPool()
	if pool == nil {
		return fmt.Errorf("database not connected")
	}
	_, err := pool.Exec(ctx, query, args...)
	return err
}

func (p *Plugin) startCoreEventWriter() {
	if p.coreEventLogQueue == nil {
		return
	}
	p.observeSQLLaneMetrics()

	p.workerWg.Add(1)
	go p.runCoreEventWriter()
}

func (p *Plugin) runCoreEventWriter() {
	defer p.workerWg.Done()

	ticker := time.NewTicker(p.coreEventFlushEvery)
	defer ticker.Stop()

	batch := make([]*coreEventLogEntry, 0, p.coreEventBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := p.flushCoreEventBatch(batch); err != nil {
			metrics.DatabaseErrors.Inc()
			logger.Error("SQL", "Failed to flush core event batch (%d items): %v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-p.ctx.Done():
			for {
				select {
				case entry := <-p.coreEventLogQueue:
					p.observeSQLLaneMetrics()
					if entry != nil {
						batch = append(batch, entry)
						if len(batch) >= p.coreEventBatchSize {
							flush()
						}
					}
				default:
					flush()
					return
				}
			}
		case entry := <-p.coreEventLogQueue:
			p.observeSQLLaneMetrics()
			if entry == nil {
				continue
			}
			batch = append(batch, entry)
			if len(batch) >= p.coreEventBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (p *Plugin) flushCoreEventBatch(entries []*coreEventLogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	copyFn := p.coreEventCopyFn
	if copyFn != nil {
		if err := copyFn(ctx, entries); err == nil {
			metrics.DatabaseQueries.WithLabelValues("insert").Add(float64(len(entries)))
			return nil
		} else {
			copyErrors := p.coreEventCopyErrors.Add(1)
			if copyErrors == 1 || copyErrors%100 == 0 {
				logger.Warn("SQL", "COPY batch insert failed, falling back to INSERT ... ON CONFLICT (count=%d): %v", copyErrors, err)
			}
		}
	}

	if err := p.flushCoreEventBatchWithInsert(ctx, entries); err != nil {
		return err
	}
	metrics.DatabaseQueries.WithLabelValues("insert").Add(float64(len(entries)))
	return nil
}

func (p *Plugin) flushCoreEventBatchWithInsert(ctx context.Context, entries []*coreEventLogEntry) error {
	execFn := p.coreEventExecFn
	if execFn == nil {
		execFn = p.execLog
	}

	query, args := buildCoreEventBatchInsert(entries)
	if err := execFn(ctx, query, args...); err != nil {
		return err
	}
	return nil
}

func (p *Plugin) copyCoreEventBatch(ctx context.Context, entries []*coreEventLogEntry) error {
	pool := p.getLogPool()
	if pool == nil {
		return fmt.Errorf("database not connected")
	}

	rows := make([][]interface{}, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []interface{}{
			entry.EventType,
			entry.Channel,
			entry.Timestamp,
			entry.Username,
			entry.Message,
			entry.MessageTime,
			entry.ToUser,
			entry.VideoID,
			entry.VideoType,
			entry.Title,
			entry.Duration,
			entry.RawData,
		})
	}

	_, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{"daz_core_events"},
		[]string{
			"event_type",
			"channel_name",
			"timestamp",
			"username",
			"message",
			"message_time",
			"to_user",
			"video_id",
			"video_type",
			"title",
			"duration",
			"raw_data",
		},
		pgx.CopyFromRows(rows),
	)
	return err
}

func buildCoreEventBatchInsert(entries []*coreEventLogEntry) (string, []interface{}) {
	const columnCount = 12
	base := `
		INSERT INTO daz_core_events (
			event_type, channel_name, timestamp, username, message, message_time,
			to_user, video_id, video_type, title, duration, raw_data
		)
		VALUES %s
		ON CONFLICT (channel_name, message_time, username)
		WHERE event_type = 'cytube.event.chatMsg'
		DO NOTHING
	`

	valueBlocks := make([]string, 0, len(entries))
	args := make([]interface{}, 0, len(entries)*columnCount)

	for i, entry := range entries {
		start := i*columnCount + 1
		valueBlocks = append(valueBlocks, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			start, start+1, start+2, start+3, start+4, start+5,
			start+6, start+7, start+8, start+9, start+10, start+11,
		))

		args = append(args,
			entry.EventType,
			entry.Channel,
			entry.Timestamp,
			entry.Username,
			entry.Message,
			entry.MessageTime,
			entry.ToUser,
			entry.VideoID,
			entry.VideoType,
			entry.Title,
			entry.Duration,
			entry.RawData,
		)
	}

	return fmt.Sprintf(base, strings.Join(valueBlocks, ", ")), args
}

func (p *Plugin) enqueueCoreEventLog(entry *coreEventLogEntry) {
	if entry == nil {
		return
	}

	queue := p.coreEventLogQueue
	if queue == nil {
		if err := p.flushCoreEventBatch([]*coreEventLogEntry{entry}); err != nil {
			metrics.DatabaseErrors.Inc()
			logger.Error("SQL", "Failed to sync flush core event log: %v", err)
		}
		p.observeSQLLaneMetrics()
		return
	}

	if !isCriticalCoreEvent(entry.EventType) {
		capacity := cap(queue)
		if capacity > 0 && len(queue) >= (capacity*80)/100 {
			drops := p.coreEventDrops.Add(1)
			metrics.SQLLaneDrops.WithLabelValues("core_event").Inc()
			if drops%100 == 1 {
				logger.Warn("SQL", "Dropping non-critical core event logs under pressure (dropped=%d)", drops)
			}
			p.observeSQLLaneMetrics()
			return
		}
	}

	select {
	case queue <- entry:
		p.observeSQLLaneMetrics()
		return
	default:
	}

	if isCriticalCoreEvent(entry.EventType) {
		timer := time.NewTimer(coreEventCriticalEnqueueMS * time.Millisecond)
		defer timer.Stop()

		select {
		case queue <- entry:
			p.observeSQLLaneMetrics()
			return
		case <-timer.C:
		case <-p.ctx.Done():
			return
		}
	}

	drops := p.coreEventDrops.Add(1)
	metrics.SQLLaneDrops.WithLabelValues("core_event").Inc()
	if drops%100 == 1 {
		logger.Warn("SQL", "Dropped core event logs due to full queue (dropped=%d)", drops)
	}
	p.observeSQLLaneMetrics()
}

func (p *Plugin) observeSQLLaneMetrics() {
	metrics.SQLLaneOccupancy.WithLabelValues("critical_active").Set(float64(len(p.sqlCriticalSem)))
	metrics.SQLLaneCapacity.WithLabelValues("critical_active").Set(float64(cap(p.sqlCriticalSem)))

	if p.sqlBackgroundSem == nil {
		metrics.SQLLaneOccupancy.WithLabelValues("background_active").Set(0)
		metrics.SQLLaneCapacity.WithLabelValues("background_active").Set(0)
	} else {
		metrics.SQLLaneOccupancy.WithLabelValues("background_active").Set(float64(len(p.sqlBackgroundSem)))
		metrics.SQLLaneCapacity.WithLabelValues("background_active").Set(float64(cap(p.sqlBackgroundSem)))
	}

	if p.sqlBackgroundPend == nil {
		metrics.SQLLaneOccupancy.WithLabelValues("background_pending").Set(0)
		metrics.SQLLaneCapacity.WithLabelValues("background_pending").Set(0)
	} else {
		metrics.SQLLaneOccupancy.WithLabelValues("background_pending").Set(float64(len(p.sqlBackgroundPend)))
		metrics.SQLLaneCapacity.WithLabelValues("background_pending").Set(float64(cap(p.sqlBackgroundPend)))
	}

	if p.coreEventLogQueue == nil {
		metrics.SQLLaneOccupancy.WithLabelValues("core_event_queue").Set(0)
		metrics.SQLLaneCapacity.WithLabelValues("core_event_queue").Set(0)
	} else {
		metrics.SQLLaneOccupancy.WithLabelValues("core_event_queue").Set(float64(len(p.coreEventLogQueue)))
		metrics.SQLLaneCapacity.WithLabelValues("core_event_queue").Set(float64(cap(p.coreEventLogQueue)))
	}
}

func (p *Plugin) pgStatEnabled() bool {
	return p.config.EnablePGStatStatements == nil || *p.config.EnablePGStatStatements
}

func (p *Plugin) initializePGStatStatements(ctx context.Context) {
	if !p.pgStatEnabled() || p.pool == nil {
		return
	}

	var available bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&available); err != nil {
		logger.Warn("SQL", "Unable to inspect pg_stat_statements extension state: %v", err)
		return
	}

	if !available {
		if _, err := p.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`); err != nil {
			logger.Warn("SQL", "Unable to enable pg_stat_statements (non-fatal): %v", err)
		}
		if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&available); err != nil {
			logger.Warn("SQL", "Unable to verify pg_stat_statements availability: %v", err)
			return
		}
	}

	if !available {
		logger.Warn("SQL", "pg_stat_statements is unavailable; top query reporting disabled")
		return
	}

	p.pgStatAvailable.Store(true)
	p.reportTopPGStatQueries(ctx, "startup")
}

func (p *Plugin) startPGStatReporter() {
	if !p.pgStatAvailable.Load() || p.ctx == nil {
		return
	}

	intervalSeconds := p.config.PGStatReportIntervalSec
	if intervalSeconds <= 0 {
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second

	p.workerWg.Add(1)
	go func() {
		defer p.workerWg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-p.ctx.Done():
				return
			case <-ticker.C:
				p.reportTopPGStatQueries(p.ctx, "periodic")
			}
		}
	}()
}

func (p *Plugin) reportTopPGStatQueries(ctx context.Context, reason string) {
	if !p.pgStatAvailable.Load() || p.pool == nil {
		return
	}

	topN := p.config.PGStatTopN
	if topN <= 0 {
		topN = 5
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := p.pool.Query(queryCtx, `
		SELECT query, calls, total_exec_time, mean_exec_time
		FROM pg_stat_statements
		WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
		ORDER BY total_exec_time DESC
		LIMIT $1
	`, topN)
	if err != nil {
		logger.Warn("SQL", "Unable to read pg_stat_statements (%s): %v", reason, err)
		return
	}
	defer rows.Close()

	rank := 0
	for rows.Next() {
		var text string
		var calls int64
		var totalMS float64
		var meanMS float64
		if err := rows.Scan(&text, &calls, &totalMS, &meanMS); err != nil {
			logger.Warn("SQL", "Failed scanning pg_stat_statements row: %v", err)
			continue
		}

		rank++
		trimmed := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
		if len(trimmed) > 140 {
			trimmed = trimmed[:140] + "..."
		}
		logger.Info("SQL", "pg_stat top[%d/%d] (%s): calls=%d total_ms=%.1f mean_ms=%.2f sql=%q", rank, topN, reason, calls, totalMS, meanMS, trimmed)
	}
}

func (p *Plugin) logPostgresTuningDiagnostics(ctx context.Context) {
	if p.pool == nil {
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	type settingRow struct {
		name    string
		setting string
		unit    string
	}

	rows, err := p.pool.Query(queryCtx, `
		SELECT name, setting, unit
		FROM pg_settings
		WHERE name IN ('shared_buffers', 'work_mem', 'effective_cache_size', 'max_connections')
	`)
	if err != nil {
		logger.Warn("SQL", "Unable to read pg_settings for tuning diagnostics: %v", err)
		return
	}
	defer rows.Close()

	settings := make(map[string]settingRow)
	for rows.Next() {
		var row settingRow
		if err := rows.Scan(&row.name, &row.setting, &row.unit); err != nil {
			continue
		}
		settings[row.name] = row
	}

	if shared, ok := settings["shared_buffers"]; ok {
		if bytes, err := p.pgSettingBytes(shared.setting, shared.unit); err == nil && bytes < 64*1024*1024 {
			logger.Warn("SQL", "Postgres shared_buffers appears low (%s%s). Consider >= 64MB for better ingest throughput.", shared.setting, shared.unit)
		}
	}
	if workMem, ok := settings["work_mem"]; ok {
		if bytes, err := p.pgSettingBytes(workMem.setting, workMem.unit); err == nil && bytes < 2*1024*1024 {
			logger.Warn("SQL", "Postgres work_mem appears low (%s%s). Consider >= 2MB to reduce sort/hash stalls.", workMem.setting, workMem.unit)
		}
	}
	if cacheSize, ok := settings["effective_cache_size"]; ok {
		if bytes, err := p.pgSettingBytes(cacheSize.setting, cacheSize.unit); err == nil && bytes < 256*1024*1024 {
			logger.Warn("SQL", "Postgres effective_cache_size appears low (%s%s). Consider >= 256MB when possible.", cacheSize.setting, cacheSize.unit)
		}
	}
}

func (p *Plugin) pgSettingBytes(setting, unit string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(setting), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse pg setting value %q: %w", setting, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("pg setting value must be non-negative: %q", setting)
	}

	multiplier, err := pgSettingUnitMultiplier(unit)
	if err != nil {
		return 0, err
	}

	if value > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("pg setting value %q with unit %q overflows int64 bytes", setting, unit)
	}

	return value * multiplier, nil
}

func pgSettingUnitMultiplier(unit string) (int64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "b", "byte", "bytes":
		return 1, nil
	case "kb":
		return 1024, nil
	case "8kb":
		return 8 * 1024, nil
	case "mb":
		return 1024 * 1024, nil
	case "gb":
		return 1024 * 1024 * 1024, nil
	case "tb":
		return 1024 * 1024 * 1024 * 1024, nil
	default:
		return 0, fmt.Errorf("unsupported pg setting unit %q", unit)
	}
}

func isCriticalCoreEvent(eventType string) bool {
	switch eventType {
	case "cytube.event.chatMsg",
		"cytube.event.pm",
		"cytube.event.pm.sent",
		"cytube.event.userJoin",
		"cytube.event.userLeave",
		"cytube.event.addUser",
		"cytube.event.userlist.start",
		"cytube.event.userlist.end",
		"cytube.event.login",
		"cytube.event.disconnect":
		return true
	default:
		return false
	}
}

func (p *Plugin) initializeSchema(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS daz_core_events (
			id BIGSERIAL PRIMARY KEY,
			event_type VARCHAR(50) NOT NULL,
			channel_name VARCHAR(100) NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			username VARCHAR(100),
			message TEXT,
			message_time BIGINT,
			video_id VARCHAR(255),
			video_type VARCHAR(50),
			title TEXT,
			duration INTEGER,
			raw_data JSONB NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON daz_core_events (timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_channel_type ON daz_core_events (channel_name, event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_events_username ON daz_core_events (username)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_events_chatmsg_unique ON daz_core_events (channel_name, message_time, username) WHERE event_type = 'cytube.event.chatMsg'`,

		`CREATE TABLE IF NOT EXISTS daz_chat_log (
			id BIGSERIAL PRIMARY KEY,
			username VARCHAR(100) NOT NULL,
			message TEXT NOT NULL,
			user_rank INTEGER,
			user_id VARCHAR(100),
			channel VARCHAR(100) NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_channel ON daz_chat_log (channel)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_username ON daz_chat_log (username)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_timestamp ON daz_chat_log (timestamp)`,

		`CREATE TABLE IF NOT EXISTS daz_user_activity (
			id BIGSERIAL PRIMARY KEY,
			event_type VARCHAR(50) NOT NULL,
			username VARCHAR(100) NOT NULL,
			user_rank INTEGER,
			channel VARCHAR(100) NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			raw_data JSONB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_event ON daz_user_activity (event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_channel ON daz_user_activity (channel)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_username ON daz_user_activity (username)`,

		`CREATE TABLE IF NOT EXISTS daz_plugin_logs (
			id BIGSERIAL PRIMARY KEY,
			plugin_name VARCHAR(100) NOT NULL,
			event_type VARCHAR(100) NOT NULL,
			log_data JSONB NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plugin_logs_plugin ON daz_plugin_logs (plugin_name)`,
		`CREATE INDEX IF NOT EXISTS idx_plugin_logs_event ON daz_plugin_logs (event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_plugin_logs_timestamp ON daz_plugin_logs (timestamp)`,

		`CREATE TABLE IF NOT EXISTS daz_private_messages (
			id BIGSERIAL PRIMARY KEY,
			event_type VARCHAR(50) NOT NULL,
			from_user VARCHAR(100) NOT NULL,
			to_user VARCHAR(100) NOT NULL,
			message TEXT NOT NULL,
			channel VARCHAR(100) NOT NULL,
			message_time BIGINT NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_users ON daz_private_messages (from_user, to_user)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_channel ON daz_private_messages (channel)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_time ON daz_private_messages (message_time)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_timestamp ON daz_private_messages (timestamp)`,

		// Migrations to add missing columns
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS message_time BIGINT`,
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS to_user VARCHAR(100)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_events_chatmsg_unique ON daz_core_events (channel_name, message_time, username) WHERE event_type = 'cytube.event.chatMsg'`,
		`CREATE INDEX IF NOT EXISTS idx_events_to_user ON daz_core_events (to_user)`,
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS video_id VARCHAR(255)`,
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS video_type VARCHAR(50)`,
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS title TEXT`,
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS duration INTEGER`,
	}

	for _, query := range queries {
		if _, err := p.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to execute schema query: %w", err)
		}
	}

	if err := p.applyExternalMigrations(ctx); err != nil {
		return err
	}

	logger.Debug("SQL", "Database schema initialized")
	return nil
}

func (p *Plugin) applyExternalMigrations(ctx context.Context) error {
	migrationFiles := []string{
		"scripts/sql/031_ollama_responses.sql",
		"scripts/sql/032_user_state_foundation.sql",
		"scripts/sql/033_economy_api.sql",
		"scripts/sql/033_eventfilter_command_descriptions.sql",
		"scripts/sql/034_tell_pending_unique.sql",
		"scripts/sql/035_oddjob_stats.sql",
		"scripts/sql/036_fishing_stats.sql",
		"scripts/sql/037_player_state_api.sql",
	}

	for _, migrationFile := range migrationFiles {
		migrationSQL, err := p.loadMigrationSQL(migrationFile)
		if err != nil {
			return err
		}

		if strings.TrimSpace(migrationSQL) == "" {
			continue
		}

		if _, err := p.pool.Exec(ctx, migrationSQL); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", migrationFile, err)
		}
	}

	return nil
}

func (p *Plugin) loadMigrationSQL(relativePath string) (string, error) {
	path, err := p.resolveMigrationPath(relativePath)
	if err != nil {
		return "", err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read migration %s: %w", path, err)
	}

	return string(contents), nil
}

func (p *Plugin) resolveMigrationPath(relativePath string) (string, error) {
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

func (p *Plugin) generateID() string {
	p.idCounter++
	return fmt.Sprintf("sql-%d-%d", time.Now().UnixNano(), p.idCounter)
}

func (p *Plugin) findMatchingEventTypes(pattern string) []string {
	var matches []string

	if strings.HasSuffix(pattern, "*") {
		if strings.HasPrefix(pattern, "cytube.event.") {
			base := strings.TrimSuffix(pattern, "*")
			commonTypes := []string{"chatMsg", "userJoin", "userLeave", "changeMedia", "queue", "mediaUpdate", "pm", "pm.sent", "playlist"}
			for _, t := range commonTypes {
				eventType := base + t
				matches = append(matches, eventType)
			}
		} else if pattern == "cytube.event.*" {
			matches = append(matches, "cytube.event")
		} else {
			matches = append(matches, strings.TrimSuffix(pattern, "*"))
		}
	} else {
		// Exact match
		matches = append(matches, pattern)
	}

	return matches
}

func (p *Plugin) createLoggerHandler(rule LoggerRule) framework.EventHandler {
	return func(event framework.Event) error {
		if dataEvent, ok := event.(*framework.DataEvent); ok {
			if dataEvent.Data != nil {
				return p.logEventData(rule, dataEvent)
			}
		}
		return nil
	}
}

func (p *Plugin) logEventData(rule LoggerRule, event *framework.DataEvent) error {
	p.eventsHandled++

	if p.pool == nil {
		return fmt.Errorf("database not connected")
	}

	// Validate table name to prevent SQL injection
	if !isValidTableName(rule.Table) {
		return fmt.Errorf("invalid table name: %s", rule.Table)
	}

	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()

	fields := p.extractFieldsForRule(rule, event.Data)
	fields.EventType = event.EventType
	fields.Timestamp = event.EventTime

	if rule.Table == "daz_core_events" {
		rawData, err := p.marshalRawData(event.Data)
		if err != nil {
			return err
		}

		p.enqueueCoreEventLog(&coreEventLogEntry{
			EventType:   fields.EventType,
			Timestamp:   fields.Timestamp,
			Channel:     fields.Channel,
			Username:    fields.Username,
			Message:     fields.Message,
			MessageTime: fields.MessageTime,
			ToUser:      fields.ToUser,
			VideoID:     fields.VideoID,
			VideoType:   fields.VideoType,
			Title:       fields.Title,
			Duration:    fields.Duration,
			RawData:     rawData,
		})
		return nil
	}

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	// Convert struct to columns and values for SQL
	columns := []string{}
	values := []interface{}{}
	placeholders := []string{}

	// Always include event_type and timestamp
	columns = append(columns, "event_type", "timestamp")
	values = append(values, fields.EventType, fields.Timestamp)
	placeholders = append(placeholders, "$1", "$2")
	i := 3

	// Add non-empty fields
	if fields.Username != "" {
		// Use "from_user" for PM table, "username" for others
		columnName := "username"
		if rule.Table == "daz_private_messages" {
			columnName = "from_user"
		}
		columns = append(columns, columnName)
		values = append(values, fields.Username)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.Message != "" {
		columns = append(columns, "message")
		values = append(values, fields.Message)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.UserRank != 0 {
		columns = append(columns, "user_rank")
		values = append(values, fields.UserRank)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.UserID != "" {
		columns = append(columns, "user_id")
		values = append(values, fields.UserID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.Channel != "" {
		// Use "channel" for daz_chat_log and daz_private_messages tables, "channel_name" for others
		columnName := "channel_name"
		if rule.Table == "daz_chat_log" || rule.Table == "daz_private_messages" {
			columnName = "channel"
		}
		columns = append(columns, columnName)
		values = append(values, fields.Channel)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.VideoID != "" {
		columns = append(columns, "video_id")
		values = append(values, fields.VideoID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.VideoType != "" {
		columns = append(columns, "video_type")
		values = append(values, fields.VideoType)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.Title != "" {
		columns = append(columns, "title")
		values = append(values, fields.Title)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.Duration != 0 {
		columns = append(columns, "duration")
		values = append(values, fields.Duration)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.ToUser != "" {
		columns = append(columns, "to_user")
		values = append(values, fields.ToUser)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.MessageTime != 0 {
		columns = append(columns, "message_time")
		values = append(values, fields.MessageTime)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
	}

	// Build query with ON CONFLICT for chat messages to prevent duplicates
	var query string
	if fields.EventType == "cytube.event.chatMsg" && rule.Table == "daz_core_events" {
		query = fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (channel_name, message_time, username) WHERE event_type = 'cytube.event.chatMsg' DO NOTHING",
			rule.Table,
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
		)
	} else {
		query = fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			rule.Table,
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
		)
	}

	if err := p.execLog(ctx, query, values...); err != nil {
		metrics.DatabaseErrors.Inc()
		logger.Error("SQL", "Failed to log event to %s: %v", rule.Table, err)
		return err
	}

	metrics.DatabaseQueries.WithLabelValues("insert").Inc()
	return nil
}

func (p *Plugin) marshalRawData(data *framework.EventData) ([]byte, error) {
	if data == nil {
		return nil, fmt.Errorf("missing event data")
	}

	if data.RawEvent != nil {
		return json.Marshal(data.RawEvent)
	}

	return json.Marshal(data)
}

func (p *Plugin) extractFieldsForRule(rule LoggerRule, data *framework.EventData) *LogFields {
	result := &LogFields{}

	if data.ChatMessage != nil {
		result.Username = data.ChatMessage.Username
		result.Message = data.ChatMessage.Message
		result.UserRank = data.ChatMessage.UserRank
		result.UserID = data.ChatMessage.UserID
		result.Channel = data.ChatMessage.Channel
		result.MessageTime = data.ChatMessage.MessageTime
	} else if data.PrivateMessage != nil {
		result.Username = data.PrivateMessage.FromUser
		result.Message = data.PrivateMessage.Message
		result.ToUser = data.PrivateMessage.ToUser
		result.MessageTime = data.PrivateMessage.MessageTime
		result.Channel = data.PrivateMessage.Channel
	} else if data.UserJoin != nil {
		result.Username = data.UserJoin.Username
		result.UserRank = data.UserJoin.UserRank
		result.Channel = data.UserJoin.Channel
	} else if data.UserLeave != nil {
		result.Username = data.UserLeave.Username
		result.Channel = data.UserLeave.Channel
	} else if data.VideoChange != nil {
		result.VideoID = data.VideoChange.VideoID
		result.VideoType = data.VideoChange.VideoType
		result.Title = data.VideoChange.Title
		result.Duration = data.VideoChange.Duration
		result.Channel = data.VideoChange.Channel
	}

	// If specific fields are requested, create a new LogFields with only those fields
	if len(rule.Fields) > 0 {
		filtered := &LogFields{}
		for _, field := range rule.Fields {
			switch field {
			case "event_type":
				filtered.EventType = result.EventType
			case "timestamp":
				filtered.Timestamp = result.Timestamp
			case "username":
				filtered.Username = result.Username
			case "message":
				filtered.Message = result.Message
			case "user_rank":
				filtered.UserRank = result.UserRank
			case "user_id":
				filtered.UserID = result.UserID
			case "channel":
				filtered.Channel = result.Channel
			case "video_id":
				filtered.VideoID = result.VideoID
			case "video_type":
				filtered.VideoType = result.VideoType
			case "title":
				filtered.Title = result.Title
			case "duration":
				filtered.Duration = result.Duration
			case "to_user":
				filtered.ToUser = result.ToUser
			case "message_time":
				filtered.MessageTime = result.MessageTime
			}
		}
		return filtered
	}

	return result
}
