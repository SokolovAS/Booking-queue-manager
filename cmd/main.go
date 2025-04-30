package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type BookingMessage struct {
	UserID    int    `json:"user_id"`
	UserEmail string `json:"user_email"`
	HotelData string `json:"hotel_data"`
}

func main() {
	// RabbitMQ connection info
	amqpURL := os.Getenv("AMQP_URL")     // e.g. "amqp://guest:guest@rabbitmq:5672/"
	queueName := os.Getenv("QUEUE_NAME") // e.g. "booking_inserts"

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		log.Fatalf("dial rabbitmq: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("open channel: %v", err)
	}
	defer ch.Close()

	// enable confirms
	if err := ch.Confirm(false); err != nil {
		log.Fatalf("enable confirms: %v", err)
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))

	// declare durable queue
	if _, err := ch.QueueDeclare(
		queueName, true, false, false, false, nil,
	); err != nil {
		log.Fatalf("queue declare: %v", err)
	}

	// HTTP handler publishes each booking to RabbitMQ
	http.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {
		var msg BookingMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		email := fmt.Sprintf("john+%s@example.com", uuid.New().String())
		msg.UserEmail = email

		body, _ := json.Marshal(msg)
		if err := ch.Publish(
			"",        // exchange
			queueName, // routing key
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				Timestamp:    time.Now(),
				ContentType:  "application/json",
				Body:         body,
			},
		); err != nil {
			log.Printf("publish error: %v", err)
			http.Error(w, "enqueue failed", http.StatusInternalServerError)
			return
		}

		// wait for broker confirm
		select {
		case conf := <-confirms:
			if !conf.Ack {
				log.Printf("nack: %d", conf.DeliveryTag)
				http.Error(w, "enqueue nack", http.StatusInternalServerError)
				return
			}
		case <-time.After(5 * time.Second):
			log.Println("confirm timeout")
			http.Error(w, "timeout", http.StatusGatewayTimeout)
			return
		}

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("published\n"))
	})

	addr := ":8090"
	log.Printf("booking-queue-manager listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
