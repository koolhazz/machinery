package brokers

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/RichardKnop/machinery/v1/config"
	"github.com/RichardKnop/machinery/v1/signatures"
	"github.com/streadway/amqp"
)

// AMQPBroker represents an AMQP broker
type AMQPBroker struct {
	config   *config.Config
	conn     *amqp.Connection
	channel  *amqp.Channel
	queue    amqp.Queue
	stopChan chan int
}

// NewAMQPBroker creates new AMQPConnection instance
func NewAMQPBroker(cnf *config.Config, stopChan chan int) Broker {
	return Broker(&AMQPBroker{
		config:   cnf,
		stopChan: stopChan,
	})
}

// StartConsuming enters a loop and waits for incoming messages
func (amqpBroker *AMQPBroker) StartConsuming(consumerTag string, taskProcessor TaskProcessor) (bool, error) {
	conn, channel, queue, err := open(amqpBroker.config)
	if err != nil {
		return true, err // retry true
	}

	defer close(channel, conn)

	if err := channel.Qos(
		3,     // prefetch count
		0,     // prefetch size
		false, // global
	); err != nil {
		return false, fmt.Errorf("Channel Qos: %s", err)
	}

	deliveries, err := channel.Consume(
		queue.Name,  // queue
		consumerTag, // consumer tag
		false,       // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		return false, fmt.Errorf("Queue Consume: %s", err)
	}

	log.Print("[*] Waiting for messages. To exit press CTRL+C")

	if err := amqpBroker.consume(deliveries, taskProcessor); err != nil {
		return true, err // retry true
	}

	return false, nil
}

// StopConsuming quits the loop
func (amqpBroker *AMQPBroker) StopConsuming() {
	// Notifying the quit channel stops consuming of messages
	amqpBroker.stopChan <- 1
}

// Publish places a new message on the default queue
func (amqpBroker *AMQPBroker) Publish(signature *signatures.TaskSignature) error {
	conn, channel, _, err := open(amqpBroker.config)
	if err != nil {
		return err
	}

	defer close(channel, conn)

	message, err := json.Marshal(signature)
	if err != nil {
		return fmt.Errorf("JSON Encode Message: %v", err)
	}

	signature.AdjustRoutingKey(
		amqpBroker.config.ExchangeType,
		amqpBroker.config.BindingKey,
		amqpBroker.config.DefaultQueue,
	)
	return channel.Publish(
		amqpBroker.config.Exchange, // exchange
		signature.RoutingKey,       // routing key
		false,                      // mandatory
		false,                      // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         message,
			DeliveryMode: amqp.Persistent,
		},
	)
}

// Consumes messages
func (amqpBroker *AMQPBroker) consume(deliveries <-chan amqp.Delivery, taskProcessor TaskProcessor) error {
	consumeOne := func(d amqp.Delivery) error {
		log.Printf("Received new message: %s", d.Body)

		signature := signatures.TaskSignature{}
		if err := json.Unmarshal(d.Body, &signature); err != nil {
			d.Nack(false, false) // multiple, requeue both false
			return err
		}

		d.Ack(false) // multiple false

		taskProcessor.Process(&signature)

		return nil
	}

	for {
		select {
		case d := <-deliveries:
			if err := consumeOne(d); err != nil {
				return err
			}
		case <-amqpBroker.stopChan:
			return nil
		}
	}
}

// Connects to the message queue, opens a channel, declares a queue
func open(cnf *config.Config) (*amqp.Connection, *amqp.Channel, amqp.Queue, error) {
	var conn *amqp.Connection
	var channel *amqp.Channel
	var queue amqp.Queue
	var err error

	conn, err = amqp.Dial(cnf.Broker)
	if err != nil {
		return conn, channel, queue, fmt.Errorf("Dial: %s", err)
	}

	channel, err = conn.Channel()
	if err != nil {
		return conn, channel, queue, fmt.Errorf("Channel: %s", err)
	}

	if err := channel.ExchangeDeclare(
		cnf.Exchange,     // name of the exchange
		cnf.ExchangeType, // type
		true,             // durable
		false,            // delete when complete
		false,            // internal
		false,            // noWait
		nil,              // arguments
	); err != nil {
		return conn, channel, queue, fmt.Errorf("Exchange: %s", err)
	}

	queue, err = channel.QueueDeclare(
		cnf.DefaultQueue, // name
		true,             // durable
		false,            // delete when unused
		false,            // exclusive
		false,            // no-wait
		nil,              // arguments
	)
	if err != nil {
		return conn, channel, queue, fmt.Errorf("Queue Declare: %s", err)
	}

	if err := channel.QueueBind(
		queue.Name,     // name of the queue
		cnf.BindingKey, // binding key
		cnf.Exchange,   // source exchange
		false,          // noWait
		nil,            // arguments
	); err != nil {
		return conn, channel, queue, fmt.Errorf("Queue Bind: %s", err)
	}

	return conn, channel, queue, nil
}

// Closes the connection
func close(channel *amqp.Channel, conn *amqp.Connection) error {
	if err := channel.Close(); err != nil {
		return fmt.Errorf("Channel Close: %s", err)
	}

	if err := conn.Close(); err != nil {
		return fmt.Errorf("Connection Close: %s", err)
	}

	return nil
}
