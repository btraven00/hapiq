package extractor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WorkerPool manages parallel processing of PDF extraction tasks.
type WorkerPool struct {
	ctx            context.Context
	tasks          chan ExtractionTask
	results        chan ExtractionTaskResult
	progressChan   chan ProgressUpdate
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	numWorkers     int
	totalTasks     int
	completedTasks int
	mu             sync.RWMutex
}

// ExtractionTask represents a single PDF extraction task.
type ExtractionTask struct {
	ID       string
	Filename string
	Options  ExtractionOptions
}

// ExtractionTaskResult represents the result of a PDF extraction task.
type ExtractionTaskResult struct {
	Error  error
	Result *ExtractionResult
	Task   ExtractionTask
}

// ProgressUpdate provides progress information.
type ProgressUpdate struct {
	TaskID      string
	Filename    string
	Status      TaskStatus
	Message     string
	Completed   int
	Total       int
	ElapsedTime time.Duration
}

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusProcessing TaskStatus = "processing"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
)

// NewWorkerPool creates a new worker pool with the specified number of workers.
func NewWorkerPool(numWorkers int) *WorkerPool {
	if numWorkers <= 0 {
		numWorkers = 4 // Default to 4 workers
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		numWorkers:   numWorkers,
		tasks:        make(chan ExtractionTask, numWorkers*2), // Buffer to prevent blocking
		results:      make(chan ExtractionTaskResult, numWorkers*2),
		progressChan: make(chan ProgressUpdate, 100),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start initializes and starts the worker pool.
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
}

// worker is the main worker function that processes tasks.
func (wp *WorkerPool) worker(workerID int) {
	defer wp.wg.Done()

	extractor := NewPDFExtractor(ExtractionOptions{}) // Will be overridden by task options

	for {
		select {
		case <-wp.ctx.Done():
			return
		case task, ok := <-wp.tasks:
			if !ok {
				return // Channel closed
			}

			wp.processTask(workerID, task, extractor)
		}
	}
}

// processTask processes a single extraction task.
func (wp *WorkerPool) processTask(workerID int, task ExtractionTask, extractor *PDFExtractor) {
	start := time.Now()

	// Send progress update: processing started
	wp.sendProgress(ProgressUpdate{
		TaskID:   task.ID,
		Filename: task.Filename,
		Status:   TaskStatusProcessing,
		Message:  fmt.Sprintf("Worker %d started processing", workerID),
	})

	// Update extractor options for this task
	extractor.options = task.Options

	// Process the PDF
	result, err := extractor.ExtractFromFile(task.Filename)
	elapsed := time.Since(start)

	// Update completion count
	wp.mu.Lock()
	wp.completedTasks++
	completed := wp.completedTasks
	total := wp.totalTasks
	wp.mu.Unlock()

	// Send progress update: task completed
	status := TaskStatusCompleted
	message := fmt.Sprintf("Worker %d completed in %v", workerID, elapsed)

	if err != nil {
		status = TaskStatusFailed
		message = fmt.Sprintf("Worker %d failed: %v", workerID, err)
	}

	wp.sendProgress(ProgressUpdate{
		TaskID:      task.ID,
		Filename:    task.Filename,
		Status:      status,
		Completed:   completed,
		Total:       total,
		ElapsedTime: elapsed,
		Message:     message,
	})

	// Send result
	wp.results <- ExtractionTaskResult{
		Task:   task,
		Result: result,
		Error:  err,
	}
}

// sendProgress sends a progress update if the channel is not full.
func (wp *WorkerPool) sendProgress(update ProgressUpdate) {
	select {
	case wp.progressChan <- update:
		// Progress update sent
	default:
		// Progress channel is full, skip this update to avoid blocking
	}
}

// SubmitTask submits a task to the worker pool.
func (wp *WorkerPool) SubmitTask(task ExtractionTask) {
	wp.mu.Lock()
	wp.totalTasks++
	wp.mu.Unlock()

	// Send progress update: task queued
	wp.sendProgress(ProgressUpdate{
		TaskID:   task.ID,
		Filename: task.Filename,
		Status:   TaskStatusPending,
		Message:  "Task queued for processing",
	})

	select {
	case wp.tasks <- task:
		// Task submitted successfully
	case <-wp.ctx.Done():
		// Context canceled
	}
}

// SubmitBatch submits multiple tasks at once.
func (wp *WorkerPool) SubmitBatch(tasks []ExtractionTask) {
	for _, task := range tasks {
		wp.SubmitTask(task)
	}
}

// Results returns the results channel for reading results.
func (wp *WorkerPool) Results() <-chan ExtractionTaskResult {
	return wp.results
}

// Progress returns the progress channel for reading progress updates.
func (wp *WorkerPool) Progress() <-chan ProgressUpdate {
	return wp.progressChan
}

// Wait waits for all submitted tasks to complete and closes the worker pool.
func (wp *WorkerPool) Wait() {
	close(wp.tasks) // Signal no more tasks
	wp.wg.Wait()    // Wait for all workers to finish
	close(wp.results)
	close(wp.progressChan)
}

// Shutdown gracefully shuts down the worker pool.
func (wp *WorkerPool) Shutdown() {
	wp.cancel() // Cancel context to stop workers
	wp.Wait()   // Wait for cleanup
}

// GetStats returns current processing statistics.
func (wp *WorkerPool) GetStats() WorkerPoolStats {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	return WorkerPoolStats{
		TotalTasks:     wp.totalTasks,
		CompletedTasks: wp.completedTasks,
		PendingTasks:   wp.totalTasks - wp.completedTasks,
		NumWorkers:     wp.numWorkers,
	}
}

// WorkerPoolStats provides statistics about the worker pool.
type WorkerPoolStats struct {
	TotalTasks     int `json:"total_tasks"`
	CompletedTasks int `json:"completed_tasks"`
	PendingTasks   int `json:"pending_tasks"`
	NumWorkers     int `json:"num_workers"`
}

// ProgressTracker tracks and reports progress for a batch of tasks.
type ProgressTracker struct {
	startTime    time.Time
	lastUpdate   time.Time
	taskStatuses map[string]TaskStatus
	updateCount  int
	mu           sync.RWMutex
}

// NewProgressTracker creates a new progress tracker.
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		startTime:    time.Now(),
		lastUpdate:   time.Now(),
		taskStatuses: make(map[string]TaskStatus),
	}
}

// Update updates the progress tracker with a new progress update.
func (pt *ProgressTracker) Update(update ProgressUpdate) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.taskStatuses[update.TaskID] = update.Status
	pt.lastUpdate = time.Now()
	pt.updateCount++
}

// GetSummary returns a summary of the current progress.
func (pt *ProgressTracker) GetSummary() ProgressSummary {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	summary := ProgressSummary{
		StartTime:    pt.startTime,
		LastUpdate:   pt.lastUpdate,
		ElapsedTime:  time.Since(pt.startTime),
		UpdateCount:  pt.updateCount,
		StatusCounts: make(map[TaskStatus]int),
	}

	for _, status := range pt.taskStatuses {
		summary.StatusCounts[status]++
	}

	summary.TotalTasks = len(pt.taskStatuses)

	return summary
}

// ProgressSummary provides a summary of progress tracking.
type ProgressSummary struct {
	StartTime    time.Time          `json:"start_time"`
	LastUpdate   time.Time          `json:"last_update"`
	StatusCounts map[TaskStatus]int `json:"status_counts"`
	ElapsedTime  time.Duration      `json:"elapsed_time"`
	UpdateCount  int                `json:"update_count"`
	TotalTasks   int                `json:"total_tasks"`
}

// PrintProgress prints a formatted progress report.
func (pt *ProgressTracker) PrintProgress() {
	summary := pt.GetSummary()

	completed := summary.StatusCounts[TaskStatusCompleted]
	failed := summary.StatusCounts[TaskStatusFailed]
	processing := summary.StatusCounts[TaskStatusProcessing]
	pending := summary.StatusCounts[TaskStatusPending]

	fmt.Printf("\r🔄 Progress: %d/%d completed", completed, summary.TotalTasks)

	if failed > 0 {
		fmt.Printf(" (%d failed)", failed)
	}

	if processing > 0 {
		fmt.Printf(" (%d processing)", processing)
	}

	if pending > 0 {
		fmt.Printf(" (%d pending)", pending)
	}

	if summary.TotalTasks > 0 {
		percentage := float64(completed) / float64(summary.TotalTasks) * 100
		fmt.Printf(" [%.1f%%]", percentage)
	}

	fmt.Printf(" [%v elapsed]", summary.ElapsedTime.Round(time.Second))
}

// EstimateCompletion estimates when all tasks will be completed.
func (pt *ProgressTracker) EstimateCompletion() time.Duration {
	summary := pt.GetSummary()

	completed := summary.StatusCounts[TaskStatusCompleted]
	if completed == 0 || summary.TotalTasks == 0 {
		return 0 // Cannot estimate
	}

	avgTimePerTask := summary.ElapsedTime / time.Duration(completed)
	remaining := summary.TotalTasks - completed

	return avgTimePerTask * time.Duration(remaining)
}
