[![GoDoc](https://img.shields.io/badge/godoc-reference-blue.svg "GoDoc")](http://godoc.org/github.com/RichardKnop/machinery/v1)
![Build Status](https://travis-ci.org/RichardKnop/machinery.svg?branch=master)

# Machinery

Machinery is an asynchronous task queue/job queue based on distributed message passing.

So called tasks (or jobs if you like) are executed concurrently either by many workers on many servers or multiple worker processes on a single server using Golang's goroutines.

This is an early stage project so far. Feel free to contribute.

- [First Steps](https://github.com/RichardKnop/machinery#first-steps)
- [Configuration](https://github.com/RichardKnop/machinery#configuration)
- [Server](https://github.com/RichardKnop/machinery#server)
- [Workers](https://github.com/RichardKnop/machinery#workers)
- [Tasks](https://github.com/RichardKnop/machinery#tasks)
    - [Registering Tasks](https://github.com/RichardKnop/machinery#registering-tasks)
    - [Signatures](https://github.com/RichardKnop/machinery#signatures)
    - [Sending Tasks](https://github.com/RichardKnop/machinery#sending-tasks)
    - [Keeping Results](https://github.com/RichardKnop/machinery#keeping-results)
- [Workflows](https://github.com/RichardKnop/machinery#workflows)
    - [Chains](https://github.com/RichardKnop/machinery#chains)
- [Development Setup](https://github.com/RichardKnop/machinery#development-setup)

## First Steps

Add the Machinery library to your $GOPATH/src:

```
$ go get github.com/RichardKnop/machinery
```

First, you will need to define some tasks. Look at sample tasks in `_examples/tasks/tasks.go` to see few examples.

Second, you will need to launch a worker process:

```
$ go run _examples/worker/worker.go
```

![Example worker](https://github.com/RichardKnop/machinery/blob/master/assets/example_worker.png)

Finally, once you have a worker running and waiting for tasks to consume, send some tasks:

```
$ go run _examples/send/send.go
```

You will be able to see the tasks being processed asynchronously by the worker:

![Example worker receives tasks](https://github.com/RichardKnop/machinery/blob/master/assets/example_worker_receives_tasks.png)

## Configuration

Machinery has several configuration options. Configuration is encapsulated by a Config struct and injected as a dependency to objects that need it.

```go
type Config struct {
	Broker          string `yaml:"broker"`
	ResultBackend   string `yaml:"result_backend"`
	ResultsExpireIn int    `yaml:"results_expire_in"`
	Exchange        string `yaml:"exchange"`
	ExchangeType    string `yaml:"exchange_type"`
	DefaultQueue    string `yaml:"default_queue"`
	BindingKey      string `yaml:"binding_key"`
}
```

### Broker

A message broker. Currently only AMQP is supported. Use full AMQP URL such as `amqp://guest:guest@localhost:5672/`.

### ResultBackend

Result backend to use for keeping task states and results. This setting is optional, you can run Machinery without keeping track of task results.

* Use `amqp` which will assume full AMQP connection details from Broker setting.
* To use Memcache backend: `memcache://10.0.0.1:11211,10.0.0.2:11211`.

### ResultsExpireIn

How long to store task results for in seconds. Defaults to 3600 (1 hour).

### Exchange

Exchange name, e.g. `machinery_exchange`.

### ExchangeType

Exchange type, e.g. `direct`.

### DefaultQueue

Default queue name, e.g. `machinery_tasks`.

### BindingKey

The queue is bind to the exchange with this key, e.g. `machinery_task`.

## Server

A Machinery library must be instantiated before use. The way this is done is by creating a Server instance. Server is a base object which stores Machinery configuration and registered tasks. E.g.:

```go

import (
    "github.com/RichardKnop/machinery/v1/config"
    machinery "github.com/RichardKnop/machinery/v1"
)

var cnf = config.Config{
	Broker:        "amqp://guest:guest@localhost:5672/",
    ResultBackend: "amqp",
	Exchange:      "machinery_exchange",
	ExchangeType:  "direct",
	DefaultQueue:  "machinery_tasks",
	BindingKey:    "machinery_task",
}

server, err := machinery.NewServer(&cnf)
if err != nil {
    // do something with the error
}
```

## Workers

In order to consume tasks, you need to have one or more workers running. All you need to run a worker is a Server instance with registered tasks. E.g.:

```go
worker := server.NewWorker("worker_name")
err := worker.Launch()
if err != nil {
    // do something with the error
}
```

Each worker will only consume registered tasks.

## Tasks

Tasks are a building block of Machinery applications. A task is a function which defines what happens when a worker receives a message. Let's say we want to define tasks for adding and multiplying numbers:

```go
func Add(args ...int64) (int64, error) {
	sum := int64(0)
	for _, arg := range args {
		sum += arg
	}
	return sum, nil
}

func Multiply(args ...int64) (int64, error) {
	sum := int64(1)
	for _, arg := range args {
		sum *= arg
	}
	return sum, nil
}
```

### Registering Tasks

Before your workers can consume a task, you need to register it with the server. This is done by assigning a task a unique name:

```go
server.RegisterTasks(map[string]interface{}{
    "add":      Add,
    "multiply": Multiply,
})
```

Task can also be registered one by one:

```go
server.RegisterTask("add", Add)
server.RegisterTask("multiply", Multiply)
```

Simply put, when a worker receives a message like this:

```json
{
    "UUID": "48760a1a-8576-4536-973b-da09048c2ac5",
    "Name": "add",
    "Args": [
        {
            "Type": "int64",
            "Value": 1,
        },
        {
            "Type": "int64",
            "Value": 1,
        }
    ],
    "Immutable": false,
    "OnSuccess": null,
    "OnError": null
}
```

It will call Add(1, 1). Each task should return an error as well so we can handle failures.

Ideally, tasks should be idempotent which means there will be no unintended consequences when a task is called multiple times with the same arguments.

### Signatures

A signature wraps calling arguments, execution options (such as immutability) and success/error callbacks of a task so it can be send across the wire to workers. Task signatures implement a simple interface:

```go
type TaskArg struct {
	Type  string
	Value interface{}
}

type TaskSignature struct {
	UUID       string
	Name       string
	RoutingKey string
	Args       []TaskArg
	Immutable  bool
	OnSuccess  []*TaskSignature
	OnError    []*TaskSignature
}
```

UUID is a unique ID of a task. You can either set it yourself or it will be automatically generated.

Name is the unique task name by which it is registered against a Server instance.

RoutingKey is used for routing a task to correct queue. If you leave it empty, the default behaviour will be to set it to the default queue's binding key for direct exchange type and to the default queue name for other exchange types.

Args is a list of arguments that will be passed to the task when it is executed by a worker.

Immutable is a flag which defines whether a result of the executed task can be modified or not. This is important with OnSuccess callbacks. Immutable task will not pass its result to its success callbacks while a mutable task will prepend its result to args sent to callback tasks. Long story short, set Immutable to false if you want to pass result of the first task in a chain to the second task.

OnSuccess defines tasks which will be called after the task has executed successfully. It is a slice of task signature structs.

OnError defines tasks which will be called after the task execution fails. The first argument passed to error callbacks will be the error returned from the failed task.

### Sending Tasks

Tasks can be called by passing an instance of TaskSignature to an App instance. E.g:

```go
import "github.com/RichardKnop/machinery/v1/signatures"

task := signatures.TaskSignature{
    Name: "add",
    Args: []signatures.TaskArg{
        signatures.TaskArg{
            Type:  "int64",
            Value: 1,
        },
        signatures.TaskArg{
            Type:  "int64",
            Value: 1,
        },
    },
}

asyncResult, err := server.SendTask(&task1)
if err != nil {
    // failed to send the task
    // do something with the error
}
```

### Keeping Results

If you have configured a result backend, the task states will be persisted. Possible states:

```go
const (
	PendingState  = "PENDING"
	ReceivedState = "RECEIVED"
	StartedState  = "STARTED"
	SuccessState  = "SUCCESS"
	FailureState  = "FAILURE"
)
```

> When using AMQP as a result backend, task states will be persisted in separate queues for each task. Although RabbitMQ can scale up to thousands of queues, it is strongly advised to use a better suited result backend (e.g. Memcache) when you are expecting to run a large number of parallel tasks.

```go
type TaskResult struct {
	Type  string
	Value interface{}
}

type TaskState struct {
	TaskUUID string
	State    string
	Result   *TaskResult
}
```

TaskState struct will be serialized and stored every time a task state changes.

AsyncResult object allows you to check for the state of a task:

```go
taskState := asyncResult.GetState()
fmt.Printf("Current state of %v task is:\n", taskState.TaskUUID)
fmt.Println(taskState.State)
```

There are couple of convenient me methods to inspect the task status:

```go
asyncResult.GetState().IsCompleted()
asyncResult.GetState().IsSuccess()
asyncResult.GetState().IsFailure()
```

You can also do a synchronous blocking call to wait for a task result:

```go
result, err := asyncResult.Get()
if err != nil {
    // task failed
    // do something with the error
}
fmt.Println(result.Interface())
```

## Workflows

Running a single asynchronous task is fine but often you will want to design a workflow of tasks to be executed in an orchestrated way. There are couple of useful functions to help you design workflows.

### Chains

Chain is simply a set of tasks which will be executed one by one, each successful task triggering the next task in the chain. E.g.:

```go
import (
    "github.com/RichardKnop/machinery/v1/signatures"
    machinery "github.com/RichardKnop/machinery/v1"
)

task1 := signatures.TaskSignature{
    Name: "add",
    Args: []signatures.TaskArg{
        signatures.TaskArg{
            Type:  "int64",
            Value: 1,
        },
        signatures.TaskArg{
            Type:  "int64",
            Value: 1,
        },
    },
}

task2 := signatures.TaskSignature{
    Name: "add",
    Args: []signatures.TaskArg{
        signatures.TaskArg{
            Type:  "int64",
            Value: 5,
        },
        signatures.TaskArg{
            Type:  "int64",
            Value: 5,
        },
    },
}

task3 := signatures.TaskSignature{
    Name: "multiply",
    Args: []signatures.TaskArg{
        signatures.TaskArg{
            Type:  "int64",
            Value: 4,
        },
    },
}

chain := machinery.NewChain(&task1, &task2, &task3)
chainAsyncResult, err := server.SendChain(chain)
if err != nil {
    // failed to send the task
    // do something with the error
}
```

The above example execute task1, then task2 and then task3, passing result of each task to the next task in the chain. Therefor what would end up happening is:

```
((1 + 1) + (5 + 6)) * 4 = 13 * 4 = 52
```

SendChain returns ChainAsyncResult which follows AsyncResult's interface. So you can do a blocking call and wait for the result of the whole chain:

```go
result, err := chainAsyncResult.Get()
if err != nil {
    // task chain failed
    // do something with the error
}
fmt.Println(result.Interface())
```

## Development Setup

First, there are several requirements:

- RabbitMQ
- Go

On OS X systems, you can install them using Homebrew:

```
$ brew install rabbitmq
$ brew install go
```

Then get all Machinery dependencies.

```
$ make deps
```

### Tests

```
$ make test
```
