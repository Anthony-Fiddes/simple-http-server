package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path"
	"strings"
)

type HTTPResponseHead struct {
	version string
	status  int
	reason  string
	headers map[string]string
}

func (h HTTPResponseHead) Bytes() []byte {
	if h.version == "" {
		h.version = "HTTP/1.1"
	}

	var result bytes.Buffer
	result.WriteString(fmt.Sprintf("%s %d", h.version, h.status))
	result.WriteString(" ")
	if h.reason != "" {
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

	return result.Bytes()
}

var okResponse = HTTPResponseHead{status: 200, reason: "OK"}
var notFoundResponse = HTTPResponseHead{status: 404, reason: "Not found"}

func handleRequest(conn net.Conn, directory string) error {
	scanner := bufio.NewScanner(conn)
	// we should be able to scan at least one line
	if !scanner.Scan() {
		return fmt.Errorf("read from connection: %w", scanner.Err())
	}
	// startLine would look like "GET /index.html HTTP/1.1"
	startLine := scanner.Text()
	sl := strings.Split(startLine, " ")
	requestPath := sl[1]

	headers := make(map[string]string)
	for scanner.Scan() {
		line := scanner.Text()
		// there are no more headers to read
		if line == "" {
			break
		}

		h := strings.Split(line, ": ")
		key := h[0]
		value := h[1]
		headers[key] = value
	}
	if scanner.Err() != nil {
		return fmt.Errorf("read headers from connection: %w", scanner.Err())
	}

	if strings.HasPrefix(requestPath, "/echo/") {
		arg := requestPath[len("/echo/"):]
		headers := make(map[string]string, 2)
		headers["Content-Type"] = "text/plain"
		headers["Content-Length"] = fmt.Sprintf("%d", len(arg))
		responseHead := okResponse
		responseHead.headers = headers
		_, err := conn.Write(responseHead.Bytes())
		if err != nil {
			return err
		}
		_, err = conn.Write([]byte(arg))
		if err != nil {
			return err
		}
	} else if strings.HasPrefix(requestPath, "/files/") {
		fileName := requestPath[len("/files/"):]
		file, err := os.Open(path.Join(directory, fileName))
		if errors.Is(err, fs.ErrNotExist) {
			conn.Write(notFoundResponse.Bytes())
			return nil
		}
		if err != nil {
			return err
		}
		defer file.Close()
		stats, err := os.Stat(fileName)
		if err != nil {
			return err
		}

		headers := make(map[string]string, 2)
		headers["Content-Type"] = "application/octet-stream"
		headers["Content-Length"] = fmt.Sprintf("%d", stats.Size())
		responseHead := okResponse
		responseHead.headers = headers
		_, err = conn.Write(responseHead.Bytes())
		if err != nil {
			return err
		}
		_, err = io.Copy(conn, file)
		if err != nil {
			return err
		}
	} else if requestPath == "/user-agent" {
		// it's okay if it's not in headers, we'll just get ""
		userAgent := headers["User-Agent"]
		headers := make(map[string]string, 2)
		headers["Content-Type"] = "text/plain"
		headers["Content-Length"] = fmt.Sprintf("%d", len(userAgent))
		responseHead := okResponse
		responseHead.headers = headers
		_, err := conn.Write(responseHead.Bytes())
		if err != nil {
			return err
		}
		_, err = conn.Write([]byte(userAgent))
		if err != nil {
			return err
		}
	} else if requestPath == "/" {
		_, err := conn.Write(okResponse.Bytes())
		if err != nil {
			return err
		}
	} else {
		_, err := conn.Write(notFoundResponse.Bytes())
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	directory := flag.String("directory", ".", "Directory to serve.")
	flag.Parse()

	l, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		log.Fatal("Failed to bind to port 4221")
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			// don't get blocked on logging
			go func() {
				log.Print("Error accepting connection: ", err.Error())
			}()
			continue
		}

		go func() {
			defer conn.Close()
			err := handleRequest(conn, *directory)
			if err != nil {
				log.Printf("Error handling request: %s", err)
			}
		}()
	}

}
