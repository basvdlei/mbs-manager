package bedrock

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
)

// console represents the server console. Allowing for concurrent
// reading/writing.
type console struct {
	// messages written to the console
	input io.Writer
	// messages printed on the console
	output io.Reader

	// inputQueue seriales command send to the console
	inputQueue chan string

	mutex sync.RWMutex
	// subs receive output messages from the console
	subs map[chan<- string]bool
	// stop will stop the reading of output messages
	stop chan struct{}
}

// newConsole creates a new console with the given in- and output that allows
// for concurrent reading and writing.
// Callers must close the console when finished with it.
func newConsole(in io.Writer, out io.Reader) *console {
	c := &console{
		input:      in,
		output:     out,
		inputQueue: make(chan string),

		mutex: sync.RWMutex{},
		subs:  make(map[chan<- string]bool),
		stop:  make(chan struct{}),
	}
	go c.readOutput()
	go func() {
		for m := range c.inputQueue {
			fmt.Fprintf(c.input, "%s\n", m)
		}
	}()
	return c
}

// Close the console.
func (c *console) Close() {
	close(c.inputQueue)
	close(c.stop)
}

// readOutput reads all messages written to the console output and broadcasts
// it to all subscribers.
func (c *console) readOutput() {
	s := bufio.NewScanner(c.output)
	for s.Scan() {
		c.BroadcastMessage(s.Text())
		select {
		case <-c.stop:
			return
		default:
		}
	}
	if err := s.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading console output:", err)
	}
}

// SendRawCommand sends the given string directly to the server console.
func (c *console) SendRawCommand(rawCommand string) {
	c.inputQueue <- rawCommand
}

// Subscribe will send console output messages to the given channel. The caller
// must call Unsubscribe() when it's finished receiving messages.
func (s *console) Subscribe(messages chan<- string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.subs[messages] = true
}

// Unsubscribe from receiving console output messages.
func (s *console) Unsubscribe(messages chan<- string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.subs, messages)
}

// BroadcastMessage sends a message to all subscriberes. If a subscriber can
// not receive (or buffer) the messages it will be dropped.
func (s *console) BroadcastMessage(message string) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for channel := range s.subs {
		select {
		case channel <- message:
			// TODO: put a timeout on bad subs
		}
	}
}
