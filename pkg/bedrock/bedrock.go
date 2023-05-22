// Package bedrock provides the tools to manage bedrock servers
package bedrock

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// Server represents a Minecraft Bedrock Server instance.
type Server struct {
	console *console

	// backupMutex to make sure only one backup is
	// executed at the same time.
	backupMutex sync.Mutex
}

// RunServer will start the Minecraft server and sets up 3 Go routines:
// - Read messages from the console.
// - Setup write queue to send messages to the console.
// - Wait for the server to exit and cleanup channels and goroutines.
func RunServer(command string, arg ...string) (*Server, <-chan error) {
	cmd := exec.Command(command, arg...)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	s := &Server{
		backupMutex: sync.Mutex{},
	}
	s.console = newConsole(in, out)
	cmd.Start()
	messages := make(chan string)
	go s.console.Subscribe(messages)
	go s.processMessages(messages)
	errChan := make(chan error)
	go func() {
		errChan <- cmd.Wait()
		close(errChan)
		close(messages)
		s.console.Close()
	}()
	return s, errChan
}

// processMessages handles the messages printed on the server console.
// Currently only prints the messages to the console.
func (s *Server) processMessages(messages <-chan string) {
	for m := range messages {
		log.Printf("[MBS<-]: %s\n", m)
	}
}

// SendRawCommand sends the given string directly to the server console.
func (s *Server) SendRawCommand(rawCommand string) {
	log.Printf("[MBS->]: %s\n", rawCommand)
	s.console.SendRawCommand(rawCommand)
}

// SendRawCommandWaitResponse sends a command and returns when the response is
// returned or the context is canceled.
// response is a regex that can match over multiple lines.
func (s *Server) SendRawCommandWaitResponse(
	ctx context.Context, rawCommand, response string) (output string, err error) {

	respRegex, err := regexp.Compile(response)
	if err != nil {
		return output, err
	}

	messages := make(chan string)
	defer close(messages)
	s.console.Subscribe(messages)
	defer s.console.Unsubscribe(messages)
	s.console.SendRawCommand(rawCommand)

	for {
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		case m := <-messages:
			if output == "" {
				output = m
			} else {
				output += "\n" + m
			}
			if respRegex.MatchString(output) {
				return output, nil
			}
		}
	}
}

func (s *Server) Attach(in io.Reader, out io.Writer) error {
	messages := make(chan string)
	s.console.Subscribe(messages)
	defer s.console.Unsubscribe(messages)
	go func() {
		for m := range messages {
			fmt.Fprintln(out, m)
		}
	}()
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		s.SendRawCommand(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// Stop the server gracefully.
func (s *Server) Stop() {
	s.console.SendRawCommand("stop")
}

func (s *Server) saveHold(ctx context.Context, timeout time.Duration) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := s.SendRawCommandWaitResponse(c, "save hold", `(?m)^Saving\.\.\.`)
	return err
}

func (s *Server) saveQuery(ctx context.Context, timeout time.Duration) (string, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := s.SendRawCommandWaitResponse(c, "save query",
		`(?s)Data saved. Files are now ready to be copied.*levelname.txt`)
	return output, err
}

func (s *Server) saveResume(ctx context.Context, timeout time.Duration) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := s.SendRawCommandWaitResponse(c, "save resume", `(?m)^Changes to the world are resumed`)
	return err
}
