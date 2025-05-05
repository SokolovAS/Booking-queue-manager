package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// BookingMessage is the structure we send.
type BookingMessage struct {
	UserID    int    `json:"user_id"`
	UserEmail string `json:"user_email"`
	HotelData string `json:"hotel_data"`
}

// Publisher manages a single AMQP connection and recreates it on failure.
type Publisher struct {
	url, queueName string

	mu   sync.Mutex
	conn *amqp.Connection
}

// NewPublisher creates a Publisher and establishes the first connection.
func NewPublisher(url, queueName string) (*Publisher, error) {
	p := &Publisher{url: url, queueName: queueName}
	if err := p.reconnectWithBackoff(); err != nil {
		return nil, err
	}
	return p, nil
}

// reconnectWithBackoff attempts to Dial until successful, with 5s intervals.
func (p *Publisher) reconnectWithBackoff() error {
	for {
		conn, err := amqp.Dial(p.url)
		if err != nil {
			log.Printf("⚠️  RabbitMQ dial failed: %v; retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		p.mu.Lock()
		p.conn = conn
		p.mu.Unlock()
		log.Println("✅ RabbitMQ connected")
		return nil
	}
}

// getConn returns a live connection, reconnecting if needed.
func (p *Publisher) getConn() *amqp.Connection {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()
	if conn == nil || conn.IsClosed() {
		if err := p.reconnectWithBackoff(); err != nil {
			log.Fatalf("fatal: cannot reconnect to RabbitMQ: %v", err)
		}
		p.mu.Lock()
		conn = p.conn
		p.mu.Unlock()
	}
	return conn
}

// Publish opens a fresh channel, declares the queue, publishes with confirms,
// and closes the channel. On failure it reconnects and retries once.
func (p *Publisher) Publish(body []byte) error {
	// Inner publish logic
	do := func() error {
		ch, err := p.getConn().Channel()
		if err != nil {
			return fmt.Errorf("channel open: %w", err)
		}
		defer ch.Close()

		// Publisher confirms
		if err := ch.Confirm(false); err != nil {
			return fmt.Errorf("confirm mode: %w", err)
		}
		confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))

		// Declare durable queue
		if _, err := ch.QueueDeclare(
			p.queueName,
			true,  // durable
			false, // auto-delete
			false, // exclusive
			false, // no-wait
			nil,
		); err != nil {
			return fmt.Errorf("queue declare: %w", err)
		}

		// Publish
		if err := ch.Publish(
			"",          // default exchange
			p.queueName, // routing key
			false,       // mandatory
			false,       // immediate
			amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				Timestamp:    time.Now(),
				ContentType:  "application/json",
				Body:         body,
			},
		); err != nil {
			return fmt.Errorf("publish: %w", err)
		}

		// Wait for confirm
		select {
		case conf := <-confirms:
			if !conf.Ack {
				return fmt.Errorf("nack received for tag %d", conf.DeliveryTag)
			}
		case <-time.After(5 * time.Second):
			return fmt.Errorf("confirm timeout")
		}
		return nil
	}

	// First attempt
	if err := do(); err != nil {
		log.Printf("⚠️ publish failed: %v; reconnecting and retrying...", err)
		if err2 := p.reconnectWithBackoff(); err2 != nil {
			return fmt.Errorf("reconnect failed: %w", err2)
		}
		// Retry once
		if err2 := do(); err2 != nil {
			return fmt.Errorf("publish after reconnect failed: %w", err2)
		}
	}
	return nil
}

func main() {
	amqpURL := os.Getenv("AMQP_URL")
	queueName := os.Getenv("QUEUE_NAME")
	if amqpURL == "" || queueName == "" {
		log.Fatal("AMQP_URL and QUEUE_NAME must be set")
	}

	pub, err := NewPublisher(amqpURL, queueName)
	if err != nil {
		log.Fatalf("failed to create publisher: %v", err)
	}

	http.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {
		var msg BookingMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// generate a unique email for testing
		msg.UserEmail = fmt.Sprintf("john+%s@example.com", uuid.New().String())
		body, _ := json.Marshal(msg)

		if err := pub.Publish(body); err != nil {
			log.Printf("❌ publish error: %v", err)
			http.Error(w, "enqueue failed", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("published\n"))
	})

	addr := ":8090"
	log.Printf("booking-queue-manager listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
