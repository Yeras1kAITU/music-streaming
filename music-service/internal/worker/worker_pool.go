package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

type Job struct {
	ID      string
	Type    string
	Payload map[string]interface{}
}

type WorkerPool struct {
	workers  int
	jobQueue chan Job
	wg       sync.WaitGroup
	redis    *redis.Client
	stopChan chan struct{}
	once     sync.Once
}

func NewWorkerPool(workers int, redis *redis.Client) *WorkerPool {
	return &WorkerPool{
		workers:  workers,
		jobQueue: make(chan Job, 100),
		redis:    redis,
		stopChan: make(chan struct{}),
	}
}

func (wp *WorkerPool) Start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
	log.Printf("Worker pool started with %d workers", wp.workers)
}

func (wp *WorkerPool) Stop() {
	wp.once.Do(func() {
		close(wp.stopChan)
		close(wp.jobQueue)
	})
	wp.wg.Wait()
	log.Println("Worker pool stopped")
}

func (wp *WorkerPool) Enqueue(job Job) {
	select {
	case wp.jobQueue <- job:
		log.Printf("Job %s enqueued", job.ID)
	default:
		log.Printf("Job queue full, dropping job %s", job.ID)
	}
}

func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for job := range wp.jobQueue {
		wp.processJob(job)
	}
}

func (wp *WorkerPool) processJob(job Job) {
	// Idempotency check
	idempotencyKey := fmt.Sprintf("job:processed:%s", job.ID)
	exists, err := wp.redis.Exists(context.Background(), idempotencyKey).Result()
	if err == nil && exists > 0 {
		log.Printf("Job %s already processed, skipping", job.ID)
		return
	}

	// Process with retries
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := wp.executeJob(job); err == nil {
			// Mark as processed
			wp.redis.Set(context.Background(), idempotencyKey, "done", 24*time.Hour)
			log.Printf("Job %s completed successfully", job.ID)
			return
		}

		// Exponential backoff
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		log.Printf("Job %s failed, retrying in %v (attempt %d/%d)", job.ID, backoff, attempt+1, maxRetries)
		time.Sleep(backoff)
	}

	log.Printf("Job %s failed after %d retries", job.ID, maxRetries)
}

func (wp *WorkerPool) executeJob(job Job) error {
	switch job.Type {
	case "transcode":
		return wp.transcodeAudio(job)
	case "thumbnail":
		return wp.generateThumbnail(job)
	default:
		log.Printf("Unknown job type: %s", job.Type)
		return nil
	}
}

func (wp *WorkerPool) transcodeAudio(job Job) error {
	inputPath, ok := job.Payload["input_path"].(string)
	if !ok {
		return fmt.Errorf("invalid input path")
	}

	// Simulate transcoding
	log.Printf("Transcoding audio: %s", inputPath)
	time.Sleep(2 * time.Second)

	log.Printf("Transcoding completed: %s", inputPath)
	return nil
}

func (wp *WorkerPool) generateThumbnail(job Job) error {
	log.Printf("Generating thumbnail for job %s", job.ID)
	time.Sleep(1 * time.Second)
	return nil
}
