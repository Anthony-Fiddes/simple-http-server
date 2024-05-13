package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net"
	"strings"
)

type HTTPResponse struct {
	version string
	status  int
	reason  string
	headers map[string]string
	body string
}

func (h HTTPResponse) Bytes() []byte {
	if h.version == "" {
		h.version = "HTTP/1.1"
	}

	var result bytes.Buffer
	result.WriteString(fmt.Sprintf("%s %d", h.version, h.status))
	if h.reason != "" {
		result.WriteString(" ")
		result.WriteString(h.reason)
	}
	result.WriteString("\r\n")

	for header, val := range h.headers {
		result.WriteString(header)
		result.WriteString(": ")
		result.WriteString(val)
		result.WriteString("\r\n")
	}
	result.WriteString("\r\n")

	result.WriteString(h.body)
	return result.Bytes()
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
	 if strings.HasPrefix(requestPath, "/echo/") {
		arg := requestPath[len("/echo/"):]
		headers := make(map[string]string, 2)
		headers["Content-Type"] = "text/plain"
		headers["Content-Length"] = fmt.Sprintf("%d", len(arg))
		response := HTTPResponse{status: 200, headers: headers, body: arg}
		conn.Write(response.Bytes())
	} else if requestPath != "/" {
		response := HTTPResponse{status: 404, reason: "Not Found"}
		conn.Write([]byte(response.Bytes()))
	} else {
		okResponse := HTTPResponse{status: 200, reason: "OK"}
		conn.Write(okResponse.Bytes())
	}
}
