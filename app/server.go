package main

import (
	"fmt"
	"log"
	"net"
	"os"
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
		log.Print("Failed to bind to port 4221")
		os.Exit(1)
	}

	conn, err := l.Accept()
	defer conn.Close()
	if err != nil {
		log.Fatal("Error accepting connection: ", err.Error())
	}

	okResponse := HTTPResponse{"HTTP/1.1", 200, "OK"}
	conn.Write([]byte(okResponse.String()))
}
