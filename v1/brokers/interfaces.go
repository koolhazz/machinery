package brokers

import "github.com/RichardKnop/machinery/v1/signatures"

// Broker - a common interface for all brokers
type Broker interface {
	StartConsuming(consumerTag string, p TaskProcessor) (bool, error)
	StopConsuming()
	Publish(task *signatures.TaskSignature) error
}

// TaskProcessor - can process a delivered task
// This will probably always be a worker instance
type TaskProcessor interface {
	Process(signature *signatures.TaskSignature)
}
