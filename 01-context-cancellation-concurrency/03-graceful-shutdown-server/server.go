package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Config holds server configuration
type Config struct {
	Port            string
	WorkerPoolSize  int
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	Logger          *slog.Logger
}

// Server represents the HTTP server with background workers and cache warmer
type Server struct {
	config        Config
	httpServer    *http.Server
	workerPool    *workerPool
	cacheWarmer   *cacheWarmer
	dbConn        *dbConnection
	shutdownCh    chan struct{}
	shutdownOnce  sync.Once
	rootCtx       context.Context
	rootCancel    context.CancelFunc
	wg            sync.WaitGroup
}

// NewServer creates a new Server instance
func NewServer(config Config) *Server {
	rootCtx, rootCancel := context.WithCancel(context.Background())

	return &Server{
		config:     config,
		shutdownCh: make(chan struct{}),
		rootCtx:    rootCtx,
		rootCancel: rootCancel,
	}
}

// Start starts the server and all background components
func (s *Server) Start(ctx context.Context) error {
	s.config.Logger.Info("starting server",
		"port", s.config.Port,
		"worker_pool_size", s.config.WorkerPoolSize,
	)

	// Initialize database connection
	s.dbConn = newDBConnection(s.config.Logger)
	if err := s.dbConn.connect(); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Start cache warmer
	s.cacheWarmer = newCacheWarmer(s.rootCtx, s.config.Logger)
	s.wg.Add(1)
	go s.cacheWarmer.start(&s.wg)

	// Initialize worker pool
	s.workerPool = newWorkerPool(s.config.WorkerPoolSize, s.config.Logger)
	s.wg.Add(1)
	go s.workerPool.start(s.rootCtx, &s.wg)

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.httpServer = &http.Server{
		Addr:         ":" + s.config.Port,
		Handler:      mux,
		ReadTimeout:  s.config.RequestTimeout,
		WriteTimeout: s.config.RequestTimeout,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in a goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.config.Logger.Info("HTTP server listening", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.config.Logger.Error("HTTP server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	var shutdownErr error

	s.shutdownOnce.Do(func() {
		s.config.Logger.Info("shutting down server")

		// Cancel root context to signal all goroutines
		s.rootCancel()

		// Step 1: Stop accepting new requests
		shutdownCtx, cancel := context.WithTimeout(ctx, s.config.ShutdownTimeout)
		defer cancel()

		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.config.Logger.Error("HTTP server shutdown error", "error", err)
			shutdownErr = fmt.Errorf("HTTP server shutdown: %w", err)
		} else {
			s.config.Logger.Info("HTTP server stopped accepting new requests")
		}

		// Step 2: Drain worker pool (wait for in-flight requests)
		if err := s.workerPool.stop(shutdownCtx); err != nil {
			s.config.Logger.Error("worker pool shutdown error", "error", err)
			if shutdownErr == nil {
				shutdownErr = fmt.Errorf("worker pool shutdown: %w", err)
			}
		} else {
			s.config.Logger.Info("worker pool drained")
		}

		// Step 3: Wait for cache warmer to finish (stopped via context cancellation)
		// The cache warmer goroutine is tracked in s.wg, so we need to wait for it
		// along with the HTTP server goroutine
		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			s.config.Logger.Info("cache warmer and all goroutines finished")
		case <-shutdownCtx.Done():
			s.config.Logger.Warn("shutdown timeout exceeded while waiting for goroutines")
			if shutdownErr == nil {
				shutdownErr = fmt.Errorf("shutdown timeout exceeded")
			}
		}

		// Step 4: Close database connection (after all goroutines finished)
		if err := s.dbConn.close(); err != nil {
			s.config.Logger.Error("database close error", "error", err)
			if shutdownErr == nil {
				shutdownErr = fmt.Errorf("database close: %w", err)
			}
		} else {
			s.config.Logger.Info("database connection closed")
		}

		close(s.shutdownCh)
		s.config.Logger.Info("server shutdown complete")
	})

	return shutdownErr
}

// handleRequest handles incoming HTTP requests
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Check if server is shutting down
	select {
	case <-s.shutdownCh:
		http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		return
	default:
	}

	// Submit request to worker pool
	req := &request{
		w: w,
		r: r,
	}

	if err := s.workerPool.submit(s.rootCtx, req); err != nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
}

// request represents an HTTP request to be processed
type request struct {
	w http.ResponseWriter
	r *http.Request
}

// workerPool manages a pool of worker goroutines
type workerPool struct {
	size      int
	workers   int
	requestCh chan *request
	stopCh    chan struct{}
	logger    *slog.Logger
	wg        sync.WaitGroup
	mu        sync.Mutex
}

func newWorkerPool(size int, logger *slog.Logger) *workerPool {
	return &workerPool{
		size:      size,
		requestCh: make(chan *request, size*2), // Buffer for better throughput
		stopCh:    make(chan struct{}),
		logger:    logger,
	}
}

func (wp *workerPool) start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	// Start worker goroutines
	wp.mu.Lock()
	for i := 0; i < wp.size; i++ {
		wp.workers++
		wp.wg.Add(1)
		go wp.worker(ctx, i)
	}
	wp.mu.Unlock()

	wp.logger.Info("worker pool started", "workers", wp.size)

	// Wait for context cancellation or stop signal
	select {
	case <-ctx.Done():
		wp.logger.Info("worker pool context cancelled")
	case <-wp.stopCh:
		wp.logger.Info("worker pool stop signal received")
	}

	// Close request channel to signal workers to stop
	close(wp.requestCh)

	// Wait for all workers to finish
	wp.wg.Wait()
	wp.logger.Info("all workers finished")
}

func (wp *workerPool) worker(ctx context.Context, id int) {
	defer wp.wg.Done()

	wp.logger.Debug("worker started", "id", id)

	for {
		select {
		case <-ctx.Done():
			wp.logger.Debug("worker context cancelled", "id", id)
			return
		case req, ok := <-wp.requestCh:
			if !ok {
				wp.logger.Debug("request channel closed", "id", id)
				return
			}
			wp.processRequest(ctx, req, id)
		}
	}
}

func (wp *workerPool) processRequest(ctx context.Context, req *request, workerID int) {
	// Handle nil request (for testing)
	if req == nil || req.r == nil {
		wp.logger.Debug("skipping nil request", "worker_id", workerID)
		return
	}

	// Simulate request processing
	path := "/"
	if req.r.URL != nil {
		path = req.r.URL.Path
	}
	wp.logger.Info("processing request",
		"worker_id", workerID,
		"path", path,
		"method", req.r.Method,
	)

	// Handle nil response writer (for testing)
	if req.w == nil {
		wp.logger.Debug("skipping response (nil writer)", "worker_id", workerID)
		// Simulate work even without response writer
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
		}
		return
	}

	// Simulate some work
	select {
	case <-time.After(100 * time.Millisecond):
		req.w.WriteHeader(http.StatusOK)
		req.w.Write([]byte(fmt.Sprintf("OK - processed by worker %d\n", workerID)))
	case <-ctx.Done():
		http.Error(req.w, "Request cancelled", http.StatusRequestTimeout)
		return
	}
}

func (wp *workerPool) submit(ctx context.Context, req *request) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wp.stopCh:
		return fmt.Errorf("worker pool is shutting down")
	case wp.requestCh <- req:
		return nil
	}
}

func (wp *workerPool) stop(ctx context.Context) error {
	close(wp.stopCh)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("worker pool stop timeout: %w", ctx.Err())
	}
}

// cacheWarmer runs background cache warming tasks
type cacheWarmer struct {
	ctx    context.Context
	ticker *time.Ticker
	logger *slog.Logger
}

func newCacheWarmer(ctx context.Context, logger *slog.Logger) *cacheWarmer {
	return &cacheWarmer{
		ctx:    ctx,
		ticker: time.NewTicker(30 * time.Second),
		logger: logger,
	}
}

func (cw *cacheWarmer) start(wg *sync.WaitGroup) {
	defer wg.Done()
	defer cw.ticker.Stop() // Proper ticker cleanup

	cw.logger.Info("cache warmer started")

	for {
		select {
		case <-cw.ctx.Done():
			cw.logger.Info("cache warmer context cancelled")
			return
		case <-cw.ticker.C:
			cw.warmCache()
		}
	}
}

func (cw *cacheWarmer) warmCache() {
	cw.logger.Info("warming cache")
	// Simulate cache warming work
	time.Sleep(100 * time.Millisecond)
	cw.logger.Info("cache warmed")
}

// dbConnection represents a database connection pool
type dbConnection struct {
	conn   net.Conn
	logger *slog.Logger
	mu     sync.Mutex
}

func newDBConnection(logger *slog.Logger) *dbConnection {
	return &dbConnection{
		logger: logger,
	}
}

func (db *dbConnection) connect() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Mock database connection using net.Pipe() for testing
	// In production, this would be a real database connection pool
	conn1, conn2 := net.Pipe()

	// Keep one end, close the other
	conn2.Close()
	db.conn = conn1
	db.logger.Info("database connection established (mock)")
	return nil
}

func (db *dbConnection) close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.conn != nil {
		if err := db.conn.Close(); err != nil {
			return fmt.Errorf("failed to close database connection: %w", err)
		}
		db.conn = nil
		db.logger.Info("database connection closed")
	}
	return nil
}

