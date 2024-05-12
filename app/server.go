package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
)

type HTTPResponse struct {
	version string
	status  int
	reason  string
}

func (h HTTPResponse) String() string {
	if h.version == "" {
		h.version = "HTTP/1.1"
	}

	result := fmt.Sprintf("%s %d", h.version, h.status)
	if h.reason != "" {
		result = fmt.Sprintf("%s %s", result, h.reason)
	}
	result += "\r\n\r\n"
	return result
}

func main() {
	// You can use print statements as follows for debugging, they'll be visible when running tests.
	log.Print("Logs from your program will appear here!")

	l, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		log.Fatal("Failed to bind to port 4221")
	}
	defer l.Close()

	conn, err := l.Accept()
	defer conn.Close()
	if err != nil {
		log.Panic("Error accepting connection: ", err.Error())
	}

	scanner := bufio.NewScanner(conn)
	if err != nil {
		log.Panic("Error reading from connection")
	}
	// don't need to read anything except the start line at this point
	if !scanner.Scan() {
		log.Panic("Could not read from connection: ", scanner.Err())
	}

	// startLine would look like "GET /index.html HTTP/1.1"
	startLine := scanner.Text()
	sl := strings.Split(startLine, " ")
	requestPath := sl[1]
	if requestPath != "/" {
		response := HTTPResponse{"HTTP/1.1", 404, "Not Found"}
		conn.Write([]byte(response.String()))
	} else {
		okResponse := HTTPResponse{"HTTP/1.1", 200, "OK"}
		conn.Write([]byte(okResponse.String()))
	}
}
